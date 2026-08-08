#!/usr/bin/env node
// atomic repl Node harness.
//
// Serves one persistent interpreter session over a unix socket, speaking the
// newline-delimited-JSON protocol defined in ../protocol.go:
//
//     {"v": 1, "op": "eval"|"ping"|"reset"|"shutdown", "code": "..."}
//  -> {"v": 1, "ok": bool, "stdout": str, "stderr": str,
//      "value": str, "error": str, "truncated": bool}
//
// Every response field is always present; `value` and `error` are empty
// strings when they do not apply, never null and never omitted.
//
// Written as an ES module and materialized to disk as node_harness.mjs (see
// HarnessFilename in ../harness_embed.go): the extension, not a package.json
// this file will never sit next to, is what fixes the module system.
//
// The Go CLI is a stateless spawner/client, so this script owns its whole
// lifecycle including its idle self-termination. It reads no config — the
// idle window is resolved by Go at spawn time and handed over as
// --idle-timeout.

import fs from 'node:fs';
import net from 'node:net';
import path from 'node:path';
import util from 'node:util';
import vm from 'node:vm';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';

// Must equal ProtocolVersion in ../protocol.go. TestHarnessScripts_PinProtocolConstants
// fails when the two drift.
const PROTOCOL_VERSION = 1;

// Must equal MaxStreamBytes in ../protocol.go.
const MAX_STREAM_BYTES = 64 * 1024;

// Filename eval'd code is compiled under, so a stack trace's line numbers
// point at the submitted code.
const EVAL_FILENAME = 'repl.js';

// A client that connects and then never sends must not wedge the session.
const REQUEST_READ_TIMEOUT_MS = 30000;

// Bounds on how often the idle clock is checked.
const MIN_POLL_MS = 10;
const MAX_POLL_MS = 250;

// Bounds the wait for a shutdown response to flush before exiting, so a client
// that stops reading cannot keep the process alive.
const SHUTDOWN_FLUSH_MS = 1000;

const SELF_URL = import.meta.url;
const SELF_PATH = fileURLToPath(SELF_URL);

function parseArgs(argv) {
    const out = { socket: '', idleTimeout: NaN, meta: '' };
    for (let i = 0; i < argv.length; i++) {
        const arg = argv[i];
        const eq = arg.indexOf('=');
        const [flag, inline] = eq === -1 ? [arg, null] : [arg.slice(0, eq), arg.slice(eq + 1)];
        const value = () => {
            if (inline !== null) return inline;
            i++;
            if (i >= argv.length) fail(`missing value for ${flag}`);
            return argv[i];
        };
        switch (flag) {
            case '--socket': out.socket = value(); break;
            case '--idle-timeout': out.idleTimeout = Number(value()); break;
            case '--meta': out.meta = value(); break;
            default: fail(`unknown flag ${flag}`);
        }
    }
    if (!out.socket) fail('--socket is required');
    if (!Number.isFinite(out.idleTimeout) || out.idleTimeout <= 0) {
        fail('--idle-timeout must be a number greater than 0');
    }
    return out;
}

function fail(message) {
    process.stderr.write(`atomic repl harness: ${message}\n`);
    process.exit(2);
}

// clip returns [text, truncated] sanitized to valid UTF-8 and capped in bytes.
// Buffer.from(..., 'utf8') replaces lone surrogates a JS string can hold but
// UTF-8 cannot encode; trimPartialCodePoint then discards whatever partial
// code point the byte cut left, so the result never exceeds the cap.
function clip(text) {
    const buf = Buffer.from(String(text), 'utf8');
    if (buf.length <= MAX_STREAM_BYTES) return [buf.toString('utf8'), false];
    return [trimPartialCodePoint(buf.subarray(0, MAX_STREAM_BYTES)), true];
}

function trimPartialCodePoint(buf) {
    let continuation = 0;
    let i = buf.length - 1;
    while (i >= 0 && (buf[i] & 0xc0) === 0x80 && continuation < 3) {
        i--;
        continuation++;
    }
    if (i >= 0) {
        const lead = buf[i];
        const width = lead < 0x80 ? 1 : lead < 0xe0 ? 2 : lead < 0xf0 ? 3 : 4;
        if (continuation + 1 < width) return buf.subarray(0, i).toString('utf8');
    }
    return buf.toString('utf8');
}

function response(ok, fields = {}) {
    return {
        v: PROTOCOL_VERSION,
        ok,
        stdout: fields.stdout || '',
        stderr: fields.stderr || '',
        value: fields.value || '',
        error: fields.error || '',
        truncated: fields.truncated || false,
    };
}

function failureResponse(message) {
    return response(false, { error: clip(message)[0] });
}

class Harness {
    constructor(socketPath, metaPath, idleTimeoutSeconds) {
        this.socketPath = socketPath;
        this.metaPath = metaPath;
        this.idleNs = BigInt(Math.round(idleTimeoutSeconds * 1e9));
        this.pollMs = Math.max(MIN_POLL_MS, Math.min(MAX_POLL_MS, (idleTimeoutSeconds * 1000) / 4));
        // hrtime, not Date.now(): a wall-clock jump from suspend/resume or an
        // NTP step must not fake (or mask) an idle window.
        this.lastActivity = process.hrtime.bigint();
        this.context = this.newContext();
        this.server = null;
        this.idleTimer = null;
    }

    newContext() {
        // Resolve require against the harness cwd (the session's scope root),
        // not against wherever the script was materialized — the caller's
        // node_modules is what eval'd code means by a bare specifier.
        const sandbox = {
            console,
            process,
            Buffer,
            URL,
            URLSearchParams,
            TextEncoder,
            TextDecoder,
            setTimeout,
            clearTimeout,
            setInterval,
            clearInterval,
            setImmediate,
            clearImmediate,
            queueMicrotask,
            require: createRequire(path.join(process.cwd(), 'atomic-repl-harness.js')),
        };
        if (typeof fetch === 'function') sandbox.fetch = fetch;
        if (typeof structuredClone === 'function') sandbox.structuredClone = structuredClone;
        return vm.createContext(sandbox);
    }

    serve() {
        this.server = net.createServer((conn) => this.handleConn(conn));
        this.server.on('error', (err) => {
            process.stderr.write(`atomic repl harness: listen ${this.socketPath}: ${err.message}\n`);
            process.exit(1);
        });
        // Belt-and-suspenders: the umask makes the socket born 0600, rather
        // than relying solely on the chmod below to close the window between
        // bind creating the file and that chmod narrowing it. The underlying
        // bind/listen syscalls for a unix socket run synchronously inside
        // this call — only the 'listening' notification below is deferred —
        // so restoring the umask right after listen() returns does not also
        // affect files eval'd code creates.
        const oldUmask = process.umask(0o177);
        this.server.listen(this.socketPath, () => {
            // A session socket is code execution into a process that may hold
            // --env secrets; the house 0o755/0o644 default is world-readable.
            try {
                fs.chmodSync(this.socketPath, 0o600);
            } catch {
                // A socket that vanished between listen and chmod is already
                // unusable; the next dial reports the session dead.
            }
        });
        process.umask(oldUmask);
        this.idleTimer = setInterval(() => {
            if (process.hrtime.bigint() - this.lastActivity >= this.idleNs) this.exit(0);
        }, this.pollMs);
    }

    exit(code) {
        if (this.idleTimer) clearInterval(this.idleTimer);
        if (this.server) {
            try {
                this.server.close();
            } catch {
                // Already closing; the unlinks below are what matter.
            }
        }
        for (const target of [this.socketPath, this.metaPath]) {
            if (!target) continue;
            try {
                fs.unlinkSync(target);
            } catch {
                // Absent or already reclaimed — nothing to undo.
            }
        }
        process.exit(code);
    }

    handleConn(conn) {
        let buffered = '';
        let handled = false;
        conn.setEncoding('utf8');
        conn.setTimeout(REQUEST_READ_TIMEOUT_MS, () => conn.destroy());
        // A client that hangs up mid-exchange raises ECONNRESET/EPIPE here,
        // and an unhandled 'error' event is fatal in Node — so an abandoned
        // connection would take the whole session's state down with it. The
        // connection is already unusable; there is nothing to recover.
        conn.on('error', () => { });
        conn.on('data', (chunk) => {
            if (handled) return;
            buffered += chunk;
            const newline = buffered.indexOf('\n');
            if (newline === -1) return;
            handled = true;
            this.handleRequest(buffered.slice(0, newline), conn);
        });
    }

    handleRequest(line, conn) {
        let request;
        try {
            request = JSON.parse(line);
        } catch (err) {
            this.reply(conn, failureResponse(`malformed request: ${err.message}`));
            return;
        }
        if (request === null || typeof request !== 'object' || Array.isArray(request)) {
            this.reply(conn, failureResponse('malformed request: expected a JSON object'));
            return;
        }
        if (request.v !== PROTOCOL_VERSION) {
            this.reply(conn, failureResponse(
                `protocol version mismatch: this harness speaks v${PROTOCOL_VERSION}, `
                + `client sent v${JSON.stringify(request.v)}; run \`atomic repl stop\` then `
                + '`atomic repl start` to replace the session'));
            return;
        }

        switch (request.op) {
            case 'ping':
                this.reply(conn, response(true));
                return;
            case 'reset':
                this.context = this.newContext();
                this.reply(conn, response(true));
                return;
            case 'shutdown':
                this.reply(conn, response(true), () => this.exit(0));
                return;
            case 'eval':
                this.reply(conn, this.evaluate(request.code || ''));
                return;
            default:
                this.reply(conn, failureResponse(
                    `unknown op ${JSON.stringify(request.op)}; valid ops: eval, ping, reset, shutdown`));
        }
    }

    reply(conn, payload, afterFlush) {
        this.lastActivity = process.hrtime.bigint();
        const frame = `${JSON.stringify(payload)}\n`;
        if (!afterFlush) {
            conn.end(frame);
            return;
        }
        // A client that stops reading must not keep the process alive past the
        // shutdown it asked for.
        const guard = setTimeout(afterFlush, SHUTDOWN_FLUSH_MS);
        conn.end(frame, () => {
            clearTimeout(guard);
            afterFlush();
        });
    }

    evaluate(code) {
        const out = [];
        const err = [];
        const restore = captureStreams(out, err);
        let value = '';
        let failure = '';
        try {
            const result = vm.runInContext(code, this.context, { filename: EVAL_FILENAME });
            // undefined is what the interactive interpreter stays silent
            // about, so it reads as "no value" rather than the literal
            // string "undefined".
            if (result !== undefined) {
                value = util.inspect(result, { depth: 4, breakLength: 120, maxArrayLength: 200 });
            }
        } catch (thrown) {
            failure = formatFailure(thrown);
        } finally {
            restore();
        }

        const [stdout, cutOut] = clip(out.join(''));
        const [stderr, cutErr] = clip(err.join(''));
        // Output produced before a failure is still delivered — a log that ran
        // is evidence about how far the code got.
        if (failure) {
            return response(false, { stdout, stderr, error: failure, truncated: cutOut || cutErr });
        }
        // inspect of a large object is unbounded; cap it on the same budget so
        // one eval cannot hand the client an arbitrarily large frame.
        const [inspected, cutValue] = clip(value);
        return response(true, {
            stdout,
            stderr,
            value: inspected,
            truncated: cutOut || cutErr || cutValue,
        });
    }
}

// captureStreams redirects writes for the duration of one eval and returns the
// undo. Patching the streams (rather than handing the sandbox its own console)
// catches console.log and a direct process.stdout.write alike.
function captureStreams(out, err) {
    const originalOut = process.stdout.write;
    const originalErr = process.stderr.write;
    const sink = (bucket) => function (chunk, encoding, callback) {
        bucket.push(typeof chunk === 'string' ? chunk : Buffer.from(chunk).toString('utf8'));
        const done = typeof encoding === 'function' ? encoding : callback;
        if (typeof done === 'function') done();
        return true;
    };
    process.stdout.write = sink(out);
    process.stderr.write = sink(err);
    return () => {
        process.stdout.write = originalOut;
        process.stderr.write = originalErr;
    };
}

function formatFailure(thrown) {
    const stack = thrown && typeof thrown.stack === 'string' && thrown.stack
        ? thrown.stack
        : util.inspect(thrown);
    return clip(trimHarnessFrames(stack))[0];
}

// trimHarnessFrames cuts the stack at this harness's own frame, plus the
// node:vm frames that bridge to it, so what remains is the eval'd code's own
// trace. V8 already prefixes the failing source line and caret above the
// frames, which is left intact.
function trimHarnessFrames(stack) {
    const lines = stack.split('\n');
    let boundary = lines.findIndex((line) => line.includes(SELF_URL) || line.includes(SELF_PATH));
    if (boundary === -1) return stack;
    while (boundary > 0 && /^\s+at .*\bnode:vm[:)]/.test(lines[boundary - 1])) boundary--;
    return lines.slice(0, boundary).join('\n').replace(/\s+$/, '');
}

const args = parseArgs(process.argv.slice(2));
new Harness(args.socket, args.meta, args.idleTimeout).serve();
