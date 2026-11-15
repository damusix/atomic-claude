// Package kind names the supported harnesses. It is a leaf so packages that
// harness itself imports, such as hooks, can still refer to a kind.
package kind

// Kind names a supported harness. The capability matrix, not the kind, records
// what each one delivers.
type Kind string

const (
	Claude Kind = "claude"
	OMP    Kind = "omp"
	Codex  Kind = "codex"
)

// Valid reports whether k is a harness this binary knows.
func (k Kind) Valid() bool {
	switch k {
	case Claude, OMP, Codex:
		return true
	}
	return false
}
