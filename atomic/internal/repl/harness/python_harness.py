#!/usr/bin/env python3
"""atomic repl Python harness.

Serves one persistent interpreter session over a unix socket, speaking the
newline-delimited-JSON protocol defined in ../protocol.go:

    {"v": 1, "op": "eval"|"ping"|"reset"|"shutdown", "code": "..."}
 -> {"v": 1, "ok": bool, "stdout": str, "stderr": str,
     "value": str, "error": str, "truncated": bool}

Every response field is always present; ``value`` and ``error`` are empty
strings when they do not apply, never null and never omitted.

The Go CLI is a stateless spawner/client: it never keeps a process of its own,
so this script owns its whole lifecycle including its idle self-termination.
It reads no config — the idle window is resolved by Go at spawn time and
handed over as ``--idle-timeout``.
"""

import argparse
import ast
import contextlib
import io
import json
import linecache
import os
import socket
import sys
import time
import traceback

# Must equal ProtocolVersion in ../protocol.go. TestHarnessScripts_PinProtocolConstants
# fails when the two drift.
PROTOCOL_VERSION = 1

# Must equal MaxStreamBytes in ../protocol.go.
MAX_STREAM_BYTES = 64 * 1024

# Filename compiled code is attributed to. Registered in linecache below so a
# traceback can show the failing source line, not just its number.
EVAL_FILENAME = "<repl>"

# A client that connects and then never sends must not wedge the accept loop:
# while it is parked in readline, no other eval is served and the idle watchdog
# never runs. Bounds that to a connection the client has clearly abandoned.
REQUEST_READ_TIMEOUT = 30.0

# Bounds on how often the blocking accept wakes up to check the idle clock.
MIN_POLL_SECONDS = 0.01
MAX_POLL_SECONDS = 0.25


def _clip(text):
    """Return (text, truncated) sanitized to valid UTF-8 and capped in bytes.

    Encoding with errors="replace" drops surrogates that a str can legally
    hold but UTF-8 cannot represent (os.listdir's surrogateescape, for one).
    Decoding the cut prefix with errors="ignore" discards the partial code
    point the cut may have left, so the result is both valid UTF-8 and never
    longer than the cap.
    """
    raw = text.encode("utf-8", "replace")
    if len(raw) <= MAX_STREAM_BYTES:
        return raw.decode("utf-8", "replace"), False
    return raw[:MAX_STREAM_BYTES].decode("utf-8", "ignore"), True


class Harness(object):
    def __init__(self, socket_path, meta_path, idle_timeout):
        self.socket_path = socket_path
        self.meta_path = meta_path
        self.idle_timeout = idle_timeout
        self.namespace = self._new_namespace()
        # Monotonic, not time.time(): a wall-clock jump from suspend/resume or
        # an NTP step must not fake (or mask) an idle window.
        self.last_activity = time.monotonic()

    @staticmethod
    def _new_namespace():
        return {"__name__": "__main__", "__doc__": None, "__package__": None}

    # -- lifecycle ---------------------------------------------------------

    def serve(self):
        server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        # Belt-and-suspenders: the umask makes the socket born 0600, rather
        # than relying solely on the chmod below to close the window between
        # bind() creating the file and that chmod narrowing it. Restored
        # immediately after bind so it does not also affect files eval'd code
        # creates.
        old_umask = os.umask(0o177)
        try:
            server.bind(self.socket_path)
        except OSError as exc:
            sys.stderr.write("atomic repl harness: bind %s: %s\n" % (self.socket_path, exc))
            return 1
        finally:
            os.umask(old_umask)
        # A session socket is code execution into a process that may hold
        # --env secrets; the house 0o755/0o644 default is world-readable.
        os.chmod(self.socket_path, 0o600)
        server.listen(64)

        poll = max(MIN_POLL_SECONDS, min(MAX_POLL_SECONDS, self.idle_timeout / 4.0))
        server.settimeout(poll)
        try:
            while True:
                try:
                    conn, _ = server.accept()
                except socket.timeout:
                    conn = None
                if conn is not None:
                    try:
                        keep_serving = self._handle_conn(conn)
                    finally:
                        conn.close()
                    if not keep_serving:
                        return 0
                # Evaluated every pass, not only when accept times out: a
                # client that connects without asking anything would otherwise
                # keep accept returning, so the window would never be checked.
                if time.monotonic() - self.last_activity >= self.idle_timeout:
                    return 0
        finally:
            server.close()
            self._remove_files()

    def _remove_files(self):
        for path in (self.socket_path, self.meta_path):
            if not path:
                continue
            try:
                os.unlink(path)
            except OSError:
                pass

    # -- request handling --------------------------------------------------

    def _handle_conn(self, conn):
        """Serve one request. Returns False when the session should end."""
        conn.settimeout(REQUEST_READ_TIMEOUT)
        try:
            line = conn.makefile("rb").readline()
        except OSError:
            return True
        if not line:
            return True

        try:
            request = json.loads(line.decode("utf-8", "replace"))
        except ValueError as exc:
            self._send(conn, self._failure("malformed request: %s" % exc))
            return True
        if not isinstance(request, dict):
            self._send(conn, self._failure("malformed request: expected a JSON object"))
            return True

        version = request.get("v")
        if version != PROTOCOL_VERSION:
            self._send(conn, self._failure(
                "protocol version mismatch: this harness speaks v%d, client sent v%s; "
                "run `atomic repl stop` then `atomic repl start` to replace the session"
                % (PROTOCOL_VERSION, json.dumps(version))))
            return True

        op = request.get("op")
        if op == "ping":
            self._send(conn, self._ok())
            return True
        if op == "reset":
            self.namespace = self._new_namespace()
            self._send(conn, self._ok())
            return True
        if op == "shutdown":
            self._send(conn, self._ok())
            return False
        if op == "eval":
            self._send(conn, self._eval(request.get("code") or ""))
            return True

        self._send(conn, self._failure(
            "unknown op %s; valid ops: eval, ping, reset, shutdown" % json.dumps(op)))
        return True

    def _send(self, conn, response):
        # The idle window measures time since the last *answered* request, so
        # it is bumped here rather than once per accepted connection: a client
        # that connects and asks nothing must not hold a session open. The Node
        # harness bumps in the same place, and the shared conformance suite
        # holds both to it.
        self.last_activity = time.monotonic()
        payload = json.dumps(response, ensure_ascii=False) + "\n"
        try:
            conn.sendall(payload.encode("utf-8"))
        except OSError:
            # Client hung up mid-response. Its problem, not a reason to end
            # a session holding live state.
            pass

    # -- responses ---------------------------------------------------------

    @staticmethod
    def _response(ok, stdout="", stderr="", value="", error="", truncated=False):
        return {
            "v": PROTOCOL_VERSION,
            "ok": ok,
            "stdout": stdout,
            "stderr": stderr,
            "value": value,
            "error": error,
            "truncated": truncated,
        }

    def _ok(self):
        return self._response(True)

    def _failure(self, message):
        return self._response(False, error=_clip(message)[0])

    # -- eval --------------------------------------------------------------

    def _eval(self, code):
        # Registering the source makes the traceback show the failing line
        # itself; without it a "<repl>" frame carries only a line number.
        linecache.cache[EVAL_FILENAME] = (len(code), None, code.splitlines(True), EVAL_FILENAME)

        out, err = io.StringIO(), io.StringIO()
        value = ""
        failure = ""
        try:
            with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
                value = self._run(code)
        except BaseException as exc:  # noqa: BLE001 - a session survives any user error
            # BaseException, not Exception: a KeyboardInterrupt from the
            # client's timeout escalation and a sys.exit() in eval'd code both
            # end the eval, never the session.
            failure = self._format_failure(exc)

        stdout, cut_out = _clip(out.getvalue())
        stderr, cut_err = _clip(err.getvalue())
        # Output produced before a failure is still delivered — a print that
        # ran is evidence about how far the code got.
        if failure:
            return self._response(False, stdout=stdout, stderr=stderr,
                                  error=failure, truncated=cut_out or cut_err)
        # repr of a large object is unbounded; cap it on the same budget so one
        # eval cannot hand the client an arbitrarily large frame.
        value, cut_value = _clip(value)
        return self._response(True, stdout=stdout, stderr=stderr, value=value,
                              truncated=cut_out or cut_err or cut_value)

    def _run(self, code):
        """Execute code REPL-style; return the final expression's repr, or ""."""
        tree = ast.parse(code, EVAL_FILENAME, "exec")
        if not tree.body:
            return ""
        tail = tree.body[-1]
        if not isinstance(tail, ast.Expr):
            exec(compile(tree, EVAL_FILENAME, "exec"), self.namespace)
            return ""
        head = ast.Module(body=tree.body[:-1], type_ignores=[])
        exec(compile(head, EVAL_FILENAME, "exec"), self.namespace)
        result = eval(compile(ast.Expression(body=tail.value), EVAL_FILENAME, "eval"), self.namespace)
        # None is what the interactive interpreter stays silent about, so it
        # reads as "no value" rather than the literal string "None".
        return "" if result is None else repr(result)

    @staticmethod
    def _format_failure(exc):
        # Drop this harness's own frames so the traceback shows the eval'd code
        # and nothing else. When no user frame exists at all — a SyntaxError
        # raised by ast.parse before any of it ran — the exception-only form
        # still carries the offending line and caret.
        tb = exc.__traceback__
        while tb is not None and tb.tb_frame.f_code.co_filename != EVAL_FILENAME:
            tb = tb.tb_next
        return "".join(traceback.format_exception(type(exc), exc, tb))


def main(argv):
    parser = argparse.ArgumentParser(
        prog="atomic-repl-python-harness",
        description="Serve one persistent Python session over a unix socket.")
    parser.add_argument("--socket", required=True,
                        help="unix socket path to bind")
    parser.add_argument("--idle-timeout", type=float, required=True,
                        help="seconds without a request before self-terminating")
    parser.add_argument("--meta", default="",
                        help="session meta file to remove on exit, if any")
    args = parser.parse_args(argv)

    if args.idle_timeout <= 0:
        parser.error("--idle-timeout must be greater than 0")

    return Harness(args.socket, args.meta, args.idle_timeout).serve()


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))

# Fix for issue #74: safe input handling
