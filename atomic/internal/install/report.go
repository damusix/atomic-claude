package install

import (
	"fmt"
	"sort"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// InstanceRow is one discovered native installation, plus whether it is
// enrolled. Discovery never enrolls, so the two facts stay separate.
type InstanceRow struct {
	Instance harness.Instance `json:"instance"`
	Enrolled bool             `json:"enrolled"`
}

// List reports every discovered instance and whether the ledger records it.
func (s Steps) List() ([]InstanceRow, error) {
	reg, err := s.registry()
	if err != nil {
		return nil, err
	}
	instances, err := reg.Discover(s.Home)
	if err != nil {
		return nil, err
	}
	ledger, err := installstate.LoadLedger(ledgerPath(s.Home))
	if err != nil {
		return nil, err
	}
	rows := make([]InstanceRow, 0, len(instances))
	for _, inst := range instances {
		rows = append(rows, InstanceRow{Instance: inst, Enrolled: enrolled(ledger, inst.Target())})
	}
	return rows, nil
}

// StatusReport is the read-only status of one home: every discovered instance
// and every enrolled target with its resources compared to the selected
// generation.
type StatusReport struct {
	Instances []InstanceRow          `json:"instances"`
	Targets   []harness.TargetStatus `json:"targets"`
	// Shared holds one record per physical resource, with consumers and mere
	// visibility kept separate: an unenrolled instance that can discover a shared
	// package is not a consumer.
	Shared    []harness.Resource     `json:"shared,omitempty"`
	Retention installstate.Retention `json:"retention"`
}

// Status reports target and shared-resource state without writing anything. The
// retention section names what a full uninstall would preserve because an
// unresolved journal still references it.
func (s Steps) Status(sel Selection) (StatusReport, error) {
	reg, err := s.registry()
	if err != nil {
		return StatusReport{}, err
	}
	instances, err := reg.Discover(s.Home)
	if err != nil {
		return StatusReport{}, err
	}
	ledger, err := installstate.LoadLedger(ledgerPath(s.Home))
	if err != nil {
		return StatusReport{}, err
	}

	var report StatusReport
	for _, inst := range instances {
		report.Instances = append(report.Instances, InstanceRow{Instance: inst, Enrolled: enrolled(ledger, inst.Target())})
	}
	report.Shared = harness.Resources(ledger, instances, harness.SharedRoots(s.Home))
	for _, record := range ledger.Targets {
		target := harness.Target{Kind: harness.Kind(record.Harness), Instance: record.Instance, NativeRoot: record.NativeRoot, Status: harness.Status(record.Status)}
		if !sel.matches(target) {
			continue
		}
		status, err := targetStatus(reg, s.Home, ledger, target)
		if err != nil {
			return StatusReport{}, err
		}
		report.Targets = append(report.Targets, status)
	}
	report.Retention, err = installstate.ComputeRetention(journalsDir(s.Home), ledger)
	if err != nil {
		return StatusReport{}, err
	}
	return report, nil
}

// Diff reports the native difference between the selected generation and the
// bytes on disk for every owned resource, read-only.
func (s Steps) Diff(sel Selection) ([]harness.ResourceStatus, error) {
	reg, err := s.registry()
	if err != nil {
		return nil, err
	}
	ledger, err := installstate.LoadLedger(ledgerPath(s.Home))
	if err != nil {
		return nil, err
	}

	var out []harness.ResourceStatus
	for _, record := range ledger.Targets {
		target := harness.Target{Kind: harness.Kind(record.Harness), Instance: record.Instance, NativeRoot: record.NativeRoot}
		if !sel.matches(target) {
			continue
		}
		desired := map[string]string{}
		if adapter, err := reg.Adapter(target.Kind); err == nil {
			if plan, err := adapter.Lifecycle(s.Home).Project(target, harness.PlanRequest{}); err == nil {
				desired = harness.DesiredDigests(plan)
			}
		}
		for _, row := range ledger.Rows {
			if row.Target != target.Key() {
				continue
			}
			status, err := harness.RowStatus(row, desired[row.Resource])
			if err != nil {
				return nil, err
			}
			out = append(out, status)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Resource < out[j].Resource })
	return out, nil
}

// targetStatus compares one target's rows against the selected generation.
func targetStatus(reg *harness.Registry, home string, ledger *installstate.Ledger, target harness.Target) (harness.TargetStatus, error) {
	out := harness.TargetStatus{Target: target, Status: target.Status}
	desired := map[string]string{}
	if adapter, err := reg.Adapter(target.Kind); err == nil {
		plan, err := adapter.Lifecycle(home).Project(target, harness.PlanRequest{})
		if err == nil {
			desired = harness.DesiredDigests(plan)
			out.Blockers = plan.Blockers
		}
	}
	for _, row := range ledger.Rows {
		if row.Target != target.Key() {
			continue
		}
		status, err := harness.RowStatus(row, desired[row.Resource])
		if err != nil {
			return out, err
		}
		out.Resources = append(out.Resources, status)
	}
	sort.Slice(out.Resources, func(i, j int) bool { return out.Resources[i].Resource < out.Resources[j].Resource })
	return out, nil
}

// enrolledInstances resolves the ledger's enrolled targets as instances.
func (s Steps) enrolledInstances(sel Selection) ([]harness.Instance, error) {
	ledger, err := installstate.LoadLedger(ledgerPath(s.Home))
	if err != nil {
		return nil, err
	}
	var out []harness.Instance
	for _, record := range ledger.Targets {
		kind := harness.Kind(record.Harness)
		if sel.Kind != "" && kind != sel.Kind {
			continue
		}
		target := harness.Target{Kind: kind, Instance: record.Instance}
		if !sel.matches(target) {
			continue
		}
		out = append(out, harness.Instance{Kind: kind, ID: record.Instance, NativeRoot: record.NativeRoot, Home: s.Home})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no enrolled target matches the selection")
	}
	return out, nil
}

// matches reports whether a target satisfies the selection's kind and instance
// filters.
func (sel Selection) matches(target harness.Target) bool {
	if sel.Kind != "" && target.Kind != sel.Kind {
		return false
	}
	if sel.Instance != "" && target.Instance != sel.Instance {
		return false
	}
	return true
}

// journalsDir is where unresolved journals live.
func journalsDir(home string) string {
	return config.JournalsDir(home)
}
