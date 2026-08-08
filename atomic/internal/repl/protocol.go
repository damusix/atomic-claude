// Package repl implements atomic repl: named, persistent Python and Node
// interpreter sessions an agent drives across separate Bash calls. A session
// is a detached interpreter child running an embedded harness script that
// serves its own unix socket; the Go side is a stateless spawner and client
// with no process of its own. See docs/design/atomic-repl.md for the
// mechanism rationale and docs/spec/atomic-repl.md for the contract.
package repl

// ProtocolVersion gates every request/response frame. A harness spawned by an
// older binary can outlive an `atomic update`, so both sides carry the version
// on the wire and a mismatch fails loud — naming `repl stop` then `start` as
// the fix — rather than parsing a frame against a shape it may not have.
//
// The two harness scripts hardcode this same number (they read no Go and no
// config); TestHarnessScripts_PinProtocolConstants fails when they drift.
const ProtocolVersion = 1

// Op names, the contract for what a Request.Op may be. AllOps is the same list
// as a slice — the harnesses' "unknown op" errors enumerate it, so keep both in
// sync; TestAllOps_MatchesGoldenList fails when they diverge.
const (
	OpEval     = "eval"
	OpPing     = "ping"
	OpReset    = "reset"
	OpShutdown = "shutdown"
)

// AllOps lists every op a harness accepts.
var AllOps = []string{OpEval, OpPing, OpReset, OpShutdown}

// MaxStreamBytes caps each of Response.Stdout and Response.Stderr. The harness,
// not the client, enforces it: a runaway loop's output must be bounded before
// it crosses the socket, not after. Value (a repr/inspect that is likewise
// unbounded in principle) is capped on the same budget.
const MaxStreamBytes = 64 * 1024

// Request is one client-to-harness frame, newline-delimited JSON. Code is
// meaningful only for OpEval; the other ops ignore it.
type Request struct {
	V    int    `json:"v"`
	Op   string `json:"op"`
	Code string `json:"code"`
}

// Response is one harness-to-client frame answering a Request.
//
// No field carries omitempty, and that is the contract, not an oversight:
// every field is always present on the wire so a caller never has to
// distinguish "absent" from "empty". Value and Error are empty strings when
// they do not apply — never null, never missing. On an eval exception Error
// carries the full traceback including the failing source line, and whatever
// Stdout/Stderr the code produced before failing is still delivered rather
// than discarded.
type Response struct {
	V         int    `json:"v"`
	OK        bool   `json:"ok"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Value     string `json:"value"`
	Error     string `json:"error"`
	Truncated bool   `json:"truncated"`
}

// ExitCode is a process exit status the repl CLI terminates with. Callers
// route on the code, never on parsed prose, so the literal values are fixed
// and distinct — TestExitCodes_PinnedValues locks all eight in one place.
type ExitCode int

const (
	// ExitOK — the command succeeded.
	ExitOK ExitCode = 0
	// ExitUsage — the command was written wrong (bad flag, no code to eval).
	ExitUsage ExitCode = 1
	// ExitNotFound — no session by that name. A reaped session and a
	// never-started name are deliberately indistinguishable: the remedy
	// ("run `atomic repl start`") is the same, so no marker separates them.
	ExitNotFound ExitCode = 2
	// ExitEvalException — the evaluated code raised or threw. The command
	// itself worked; the code did not.
	ExitEvalException ExitCode = 3
	// ExitTimeout — the eval deadline elapsed and the SIGINT-then-SIGKILL
	// escalation was exhausted.
	ExitTimeout ExitCode = 4
	// ExitDead — the session's socket is unreachable (harness crashed or was
	// killed without cleaning up). Never silently restarted: a silent restart
	// would hide the state loss.
	ExitDead ExitCode = 5
	// ExitInterpreterUnavailable — --lang's interpreter is not on PATH, or an
	// explicit --bin does not resolve. Distinct from ExitUsage so an agent can
	// tell "install it or point --bin" apart from "I wrote the command wrong".
	ExitInterpreterUnavailable ExitCode = 6
	// ExitProtocolMismatch — a live harness answers with a protocol version
	// this binary does not speak (it predates an `atomic update`). Distinct
	// from ExitDead: the session is still alive, so a bare `start` would only
	// report already-running — the remedy is `repl stop` then `start`.
	ExitProtocolMismatch ExitCode = 7
)
