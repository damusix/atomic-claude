// Package repl implements atomic repl: named, persistent Python and Node
// interpreter sessions an agent drives across separate Bash calls. A session is
// a detached interpreter child running an embedded harness script that serves
// its own unix socket; the Go side is a stateless spawner and client with no
// process of its own. Contract: docs/spec/atomic-repl.md.
package repl

// ProtocolVersion gates every frame. A harness spawned by an older binary can
// outlive an `atomic update`, so both sides carry the version on the wire and a
// mismatch fails loud rather than parsing a frame against a shape it may not
// have. The two harness scripts hardcode the same number (they read no Go and no
// config); TestHarnessScripts_PinProtocolConstants fails when they drift.
const ProtocolVersion = 1

// Op names. AllOps below is the same list as a slice — the harnesses' "unknown
// op" errors enumerate it, so keep both in sync; TestAllOps_MatchesGoldenList
// fails when they diverge.
const (
	OpEval     = "eval"
	OpPing     = "ping"
	OpReset    = "reset"
	OpShutdown = "shutdown"
)

// AllOps lists every op a harness accepts.
var AllOps = []string{OpEval, OpPing, OpReset, OpShutdown}

// MaxStreamBytes caps Response.Stdout and Response.Stderr. The harness enforces
// it, not the client: a runaway loop's output must be bounded before it crosses
// the socket. Value is capped on the same budget.
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
// No field carries omitempty, and that is the contract: every field is always
// present so a caller never distinguishes "absent" from "empty". On an eval
// exception Error carries the full traceback including the failing source line,
// and whatever Stdout/Stderr the code produced before failing is still
// delivered rather than discarded.
type Response struct {
	V         int    `json:"v"`
	OK        bool   `json:"ok"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Value     string `json:"value"`
	Error     string `json:"error"`
	Truncated bool   `json:"truncated"`
}

// ExitCode is a process exit status the repl CLI terminates with. Callers route
// on the code, never on parsed prose, so the values are fixed and distinct —
// TestExitCodes_PinnedValues locks all eight in one place.
type ExitCode int

const (
	// ExitOK — the command succeeded.
	ExitOK ExitCode = 0
	// ExitUsage — the command was written wrong (bad flag, no code to eval).
	ExitUsage ExitCode = 1
	// ExitNotFound — no session by that name. A reaped session and a
	// never-started name are deliberately indistinguishable: the remedy is the
	// same, so no marker separates them.
	ExitNotFound ExitCode = 2
	// ExitEvalException — the evaluated code raised or threw. The command
	// itself worked; the code did not.
	ExitEvalException ExitCode = 3
	// ExitTimeout — the deadline elapsed and the escalation was exhausted.
	ExitTimeout ExitCode = 4
	// ExitDead — the socket is unreachable (harness crashed or was killed without
	// cleaning up). Never silently restarted: that would hide the state loss.
	ExitDead ExitCode = 5
	// ExitInterpreterUnavailable — the interpreter is not on PATH, or --bin does
	// not resolve. Distinct from ExitUsage so an agent can tell "install it or
	// point --bin" from "I wrote the command wrong".
	ExitInterpreterUnavailable ExitCode = 6
	// ExitProtocolMismatch — a live harness speaks a version this binary does
	// not. Distinct from ExitDead: the session is alive, so a bare `start` would
	// only report already-running — the remedy is `stop` then `start`.
	ExitProtocolMismatch ExitCode = 7
)
