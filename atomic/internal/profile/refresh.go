package profile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// DefaultRefreshDays is the window install, update, and the session-start hook
// all pass to RefreshIfStale. A code constant until demand for config is proven.
const DefaultRefreshDays = 1

// ParseDuration accepts only "<N>d" with N a positive integer.
func ParseDuration(s string) (int, error) {
	if !strings.HasSuffix(s, "d") {
		return 0, fmt.Errorf("profile: duration %q: only days are accepted (e.g. 7d, 30d)", s)
	}
	numStr := s[:len(s)-1]
	if numStr == "" {
		return 0, fmt.Errorf("profile: duration %q: missing numeric value before 'd'", s)
	}
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("profile: duration %q: %w", s, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("profile: duration %q: value must be > 0", s)
	}
	return n, nil
}

var lastcheckRe = regexp.MustCompile(`<deterministic\s+lastcheck=(\d{4}-\d{2}-\d{2})>`)

// ParseLastcheck returns ("", false) when the <deterministic> tag carries no
// lastcheck — a v1-format file, or no file at all.
func ParseLastcheck(content string) (string, bool) {
	m := lastcheckRe.FindStringSubmatch(content)
	if m == nil {
		return "", false
	}
	return m[1], true
}

const dateFmt = "2006-01-02"

// IsStale treats a malformed date as stale, so the fallback is always to refresh.
func IsStale(lastcheck, today string, days int) bool {
	lc, err := time.ParseInLocation(dateFmt, lastcheck, time.UTC)
	if err != nil {
		return true // malformed → treat as stale
	}
	td, err := time.ParseInLocation(dateFmt, today, time.UTC)
	if err != nil {
		return true // malformed → treat as stale
	}
	diff := td.Sub(lc)
	return diff >= time.Duration(days)*24*time.Hour
}

// Refresh autodetects workflow preferences, then rewrites ## Environment with
// date. Reports whether it wrote; an unchanged result writes nothing.
func Refresh(home, date string) (bool, error) {
	return refresh(home, date, defaultRefreshOpts(home))
}

func defaultRefreshOpts(home string) refreshOpts {
	return refreshOpts{probe: WorkflowProbe{Home: home}, stderr: os.Stderr}
}

// refreshOpts holds the seams tests replace. A nil registry means the default
// one; []RegistryEntry{} probes nothing.
type refreshOpts struct {
	probe    WorkflowProbe
	registry []RegistryEntry
	stderr   io.Writer
}

func refresh(home, date string, opts refreshOpts) (bool, error) {
	probe, stderr := opts.probe, opts.stderr
	profilePath := config.ProfilePath(home)

	content, err := readProfileFile(profilePath)
	if err != nil {
		return false, err
	}

	// Before the tool probes: a refusal leaves lastcheck stale, so each session
	// would pay for them.
	detection := DetectWorkflow(probe)
	if _, err := ApplyWorkflowDetection(content, detection); err != nil {
		fmt.Fprintf(stderr, "profile refresh: %s not updated: %v\n", profilePath, err)
		return false, nil
	}

	env := CaptureEnv()
	tools := DetectAll(DetectOptions{Registry: opts.registry})
	shell := DetectShell(ShellEnvOptions{})
	envSection := RenderEnvironmentSection(env, tools, shell, date)

	unlock, err := LockProfile(home)
	if err != nil {
		return false, fmt.Errorf("profile refresh: %w", err)
	}
	defer unlock()
	// Re-read: `atomic workflow set` may have written while the probes ran.
	if content, err = readProfileFile(profilePath); err != nil {
		return false, err
	}
	withWorkflow, err := ApplyWorkflowDetection(content, detection)
	if err != nil {
		fmt.Fprintf(stderr, "profile refresh: %s not updated: %v\n", profilePath, err)
		return false, nil
	}
	newContent := RewriteEnvironmentSection(withWorkflow, envSection)
	if newContent == content {
		return false, nil
	}

	// Temp file beside the destination, then rename — an interrupted write must
	// not truncate the user's profile.
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o755); err != nil {
		return false, fmt.Errorf("profile refresh: mkdir %s: %w", filepath.Dir(profilePath), err)
	}

	tmp := profilePath + ".tmp"
	if err := os.WriteFile(tmp, []byte(newContent), 0o644); err != nil {
		return false, fmt.Errorf("profile refresh: write tmp: %w", err)
	}
	if err := os.Rename(tmp, profilePath); err != nil {
		_ = os.Remove(tmp)
		return false, fmt.Errorf("profile refresh: rename: %w", err)
	}

	return true, nil
}

func readProfileFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("profile refresh: read %s: %w", path, err)
	}
	return string(b), nil
}

// LockProfile blocks until it holds an exclusive flock for home's profile.
// Every read-modify-write of the profile runs between it and unlock.
func LockProfile(home string) (unlock func(), err error) {
	// A separate file, never profile.md itself: writers replace profile.md by
	// rename, which would hand the next writer a different, unlocked inode.
	dir := filepath.Dir(config.ProfilePath(home))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("profile lock: mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, "profile.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("profile lock: open %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("profile lock: flock %s: %w", path, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// RefreshIfStale is a no-op while lastcheck is within days of today. A missing
// lastcheck counts as infinitely stale.
func RefreshIfStale(home, today string, days int) (bool, error) {
	return refreshIfStale(home, today, days, defaultRefreshOpts(home))
}

func refreshIfStale(home, today string, days int, opts refreshOpts) (bool, error) {
	profilePath := config.ProfilePath(home)

	existing, err := os.ReadFile(profilePath)
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("profile refresh --if-stale: read %s: %w", profilePath, err)
	}

	if err == nil {
		if lastcheck, ok := ParseLastcheck(string(existing)); ok {
			if !IsStale(lastcheck, today, days) {
				return false, nil
			}
		}
	}
	return refresh(home, today, opts)
}
