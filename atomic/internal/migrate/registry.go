package migrate

// Registry is the ordered list of all registered migrations.
// Callers pass this slice (or a subset) to Run. The slice is initially empty;
// the real first migration step (signals → docs/wiki/ relocation) is
// registered in checkpoint C4.
var Registry []Migration
