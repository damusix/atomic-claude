package installstate

// TargetRecord is one enrolled harness instance, its native root, and its last
// convergence status. Enrollment writes it; discovery never does.
type TargetRecord struct {
	Harness    string `json:"harness"`
	Instance   string `json:"instance"`
	NativeRoot string `json:"native_root"`
	Status     string `json:"status"`
}

// Key is the harness-and-instance identity keying target records and rows.
// harness.Target delegates here so the two spellings cannot drift.
func (t TargetRecord) Key() string {
	return t.Harness + ":" + t.Instance
}

// UpsertTarget inserts or replaces the enrolled record for one instance. It
// reports whether anything changed.
func (l *Ledger) UpsertTarget(t TargetRecord) bool {
	for i := range l.Targets {
		if l.Targets[i].Harness == t.Harness && l.Targets[i].Instance == t.Instance {
			if l.Targets[i] == t {
				return false
			}
			l.Targets[i] = t
			return true
		}
	}
	l.Targets = append(l.Targets, t)
	return true
}

// FindTarget returns the enrolled record for one harness instance.
func (l *Ledger) FindTarget(harness, instance string) (TargetRecord, bool) {
	for _, t := range l.Targets {
		if t.Harness == harness && t.Instance == instance {
			return t, true
		}
	}
	return TargetRecord{}, false
}
