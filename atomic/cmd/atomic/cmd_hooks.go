package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/cliutil"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
	"github.com/damusix/atomic-claude/atomic/internal/realmshare"
	"github.com/damusix/atomic-claude/atomic/internal/repoctx"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
	"github.com/spf13/cobra"
)

func buildHooksCmd(repoOverride *string) *cobra.Command {
	dispatch := func(args []string) { runHooks(args, *repoOverride) }
	parent := &cobra.Command{
		Use:   "hooks",
		Short: "Manage atomic hooks (session-start|pre-tool-use|install|uninstall)",
		Args:  cobra.ArbitraryArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { dispatch(args); return nil },
	}
	addSub := func(verb, short, argsHint string, flagFn func(*cobra.Command)) {
		c := &cobra.Command{
			Use:                verb,
			Short:              short,
			Annotations:        map[string]string{"args_hint": argsHint},
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				dispatch(append([]string{verb}, args...))
				return nil
			},
		}
		if flagFn != nil {
			flagFn(c)
		}
		parent.AddCommand(c)
	}
	addSub("session-start", "Print session-start hook payload", "", func(c *cobra.Command) {
		c.Flags().String("format", "", "output format: json or text")
		c.Flags().String("harness", "", "harness the hook runs under: claude, omp, or codex (default claude)")
		c.Flags().String("cwd", "", cwdHelp)
	})
	// Replaces the root's state-dir preflight: a refusal would fail the edit.
	addSub("pre-tool-use", "Print the wiki cards matching a Codex edit", "", func(c *cobra.Command) {
		c.PersistentPreRunE = func(*cobra.Command, []string) error { return nil }
	})
	addSub("install", "Install session-start hook", "", func(c *cobra.Command) {
		c.Flags().String("scope", "", "scope: user or project")
	})
	addSub("uninstall", "Remove session-start hook", "", func(c *cobra.Command) {
		c.Flags().String("scope", "", "scope: user or project")
	})
	return parent
}

func runHooks(args []string, repoOverride string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: atomic hooks <session-start|pre-tool-use|install|uninstall> [flags]\n")
		os.Exit(2)
	}

	verb := args[0]
	switch verb {
	case "session-start":
		fs := flag.NewFlagSet("hooks session-start", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic hooks session-start [--format json|text] [--harness claude|omp|codex] [--cwd <dir>]")
		var format, harnessFlag, cwdFlag string
		fs.StringVar(&format, "format", "json", "output format: json or text")
		fs.StringVar(&harnessFlag, "harness", "claude", "harness the hook runs under: claude, omp, or codex")
		fs.StringVar(&cwdFlag, "cwd", "", cwdHelp)
		realmPrimary := fs.Bool("realm-primary", false, "")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}
		kind, err := harness.ParseKind(harnessFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks session-start: %v\n", err)
			os.Exit(2)
		}
		harnessFlag = string(kind)

		stdin := pipedStdin(os.Stdin)
		var realmDir string
		var loadInstructions, remind bool
		if kind == harness.KindOMP && *realmPrimary && repoOverride == "" {
			realmDir = cwdFlag
			loadInstructions, remind = true, format == "text"
		}
		if (kind == harness.KindCodex || kind == harness.KindClaude) && cwdFlag == "" && repoOverride == "" && stdin != nil {
			input, err := readSessionStartInput(stdin, stdinWait, true)
			cwdFlag, stdin = input.Cwd, nil
			if err == nil && input.primaryStartup() {
				realmDir = input.Cwd
				loadInstructions = kind == harness.KindCodex
				remind = format == "json" && input.primaryAgent()
			}
		}
		root, err := resolveSessionRoot(cwdFlag, stdin, repoOverride)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks session-start: %v\n", err)
			os.Exit(1)
		}

		now := time.Now()
		out := hooks.SessionStartText(root, harnessFlag, now.UTC())
		if repoOverride == "" {
			out = orientSessionLinks(root, out)
		}
		var notice string
		if realmDir != "" && (loadInstructions || remind) {
			realm, err := sessionInstructionRealm(realmDir)
			if err == nil && realm != "" {
				if remind {
					notice = sessionRealmSyncReminder(realm, now)
				}
				if loadInstructions {
					var body string
					body, err = realmshare.SessionInstructions(realm, kind, false)
					if err == nil && body != "" {
						if out != "" {
							out += "\n\n"
						}
						out += body
					}
				}
			}
			if err != nil && loadInstructions {
				fmt.Fprintf(os.Stderr, "atomic hooks session-start: %v\n", err)
			}
		}
		if format != "text" {
			if out, err = hooks.FormatSessionStartOutput(out, notice); err != nil {
				fmt.Fprintf(os.Stderr, "atomic hooks session-start: %v\n", err)
				os.Exit(1)
			}
		} else if notice != "" {
			fmt.Fprintln(os.Stderr, "atomic-realm-sync-reminder: "+notice)
		}
		if out != "" {
			fmt.Println(out)
		}

	case "pre-tool-use":
		runPreToolUse(os.Stdin)

	case "install":
		fs := flag.NewFlagSet("hooks install", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic hooks install [--scope user|project]")
		var scope string
		fs.StringVar(&scope, "scope", "user", "scope: user or project")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		root, err := resolveHooksRoot(scope, repoOverride)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks install: %v\n", err)
			os.Exit(1)
		}

		scopeRoot, err := resolveScopeRoot(scope, root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks install: %v\n", err)
			os.Exit(1)
		}

		skipped, err := hooks.Install(root, scopeRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		if skipped {
			fmt.Fprintf(os.Stderr, "warning: settings.json is read-only; hooks not installed (scope=%s, non-fatal)\n", scope)
		} else {
			fmt.Fprintf(os.Stderr, "hooks installed (scope=%s)\n", scope)
		}

	case "uninstall":
		fs := flag.NewFlagSet("hooks uninstall", flag.ContinueOnError)
		cliutil.SetUsage(fs, "atomic hooks uninstall [--scope user|project]")
		var scope string
		fs.StringVar(&scope, "scope", "user", "scope: user or project")
		if err := fs.Parse(args[1:]); err != nil {
			os.Exit(2)
		}

		root, err := resolveHooksRoot(scope, repoOverride)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks uninstall: %v\n", err)
			os.Exit(1)
		}

		scopeRoot, err := resolveScopeRoot(scope, root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "atomic hooks uninstall: %v\n", err)
			os.Exit(1)
		}

		skipped, err := hooks.Uninstall(root, scopeRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		if skipped {
			fmt.Fprintf(os.Stderr, "warning: settings.json is read-only; hooks not uninstalled (scope=%s, non-fatal)\n", scope)
		} else {
			fmt.Fprintf(os.Stderr, "hooks uninstalled (scope=%s)\n", scope)
		}

	default:
		fmt.Fprintf(os.Stderr, "atomic hooks: unknown verb %q\n", verb)
		fmt.Fprintf(os.Stderr, "Usage: atomic hooks <session-start|pre-tool-use|install|uninstall> [flags]\n")
		os.Exit(2)
	}
}

func runClaudeRealmSessionStart(stdin io.Reader, projectDir string) error {
	input, err := readSessionStartInput(stdin, stdinWait, true)
	if err != nil {
		return err
	}
	if !input.primaryStartup() {
		return fmt.Errorf("realm instructions require a native SessionStart startup event")
	}

	root, err := sessionInstructionRealm(input.Cwd)
	if err != nil {
		return err
	}
	if projectDir != "" && projectDir != input.Cwd {
		projectRoot, err := sessionInstructionRealm(projectDir)
		if err != nil {
			return err
		}
		if projectRoot != root {
			return fmt.Errorf("Claude project directory and SessionStart cwd must be in the same local realm")
		}
	}
	if root == "" {
		return nil
	}

	body, err := realmshare.SessionInstructions(root, harness.KindClaude, true)
	var notice string
	if input.primaryAgent() {
		notice = sessionRealmSyncReminder(root, time.Now())
	}
	out, formatErr := hooks.FormatSessionStartOutput(body, notice)
	if formatErr != nil {
		return formatErr
	}
	if out != "" {
		if _, writeErr := fmt.Print(out); writeErr != nil {
			return writeErr
		}
	}
	return err
}

func sessionRealmSyncReminder(root string, now time.Time) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if realmshare.ClaimSyncReminder(ctx, root, now) {
		return "Shared realm changes are pending. Run `atomic realm sync` to publish them."
	}
	return ""
}

func sessionInstructionRealm(dir string) (string, error) {
	if !filepath.IsAbs(dir) || dir != filepath.Clean(dir) {
		return "", fmt.Errorf("realm instruction directory must be a clean absolute path")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("resolve realm instruction directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("realm instruction path %q is not a directory", dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	claudeMDPath := filepath.Join(home, ".claude", "CLAUDE.md")
	if root, linked, err := sessionLinkedRealm(dir, claudeMDPath); linked || err != nil {
		return root, err
	}
	physical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolve realm instruction directory: %w", err)
	}
	realm, found, err := wiki.FindRealm(physical, claudeMDPath)
	if err != nil || !found {
		return "", err
	}
	if physical != dir {
		logical, found, err := wiki.FindRealm(dir, claudeMDPath)
		if err != nil || !found {
			return "", err
		}
		info, err := os.Lstat(logical.Root)
		if err != nil || !info.IsDir() {
			return "", nil
		}
		resolved, err := filepath.EvalSymlinks(logical.Root)
		if err != nil || resolved != realm.Root {
			return "", err
		}
	}
	if physical != realm.Root && sessionNestedRepo(physical, realm.Root, false) {
		return "", nil
	}
	return realm.Root, nil
}

func sessionLinkedRealm(dir, claudeMDPath string) (string, bool, error) {
	root, linked, err := localRealmFromLink(dir, claudeMDPath)
	if err != nil || root == "" {
		return root, linked, err
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", true, nil
	}
	// The witnessed link is the boundary; another alias below it is unowned.
	var link string
	for path := dir; path != root && wiki.IsUnder(path, root); path = filepath.Dir(path) {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			if link != "" {
				return "", true, nil
			}
			link = path
		}
	}
	if link == "" {
		return "", true, nil
	}
	target, err := filepath.EvalSymlinks(link)
	if err != nil {
		return "", true, err
	}
	physical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", true, err
	}
	if !wiki.IsUnder(physical, target) || sessionNestedRepo(physical, target, true) {
		return "", true, nil
	}
	return root, true, nil
}

func sessionNestedRepo(dir, boundary string, includeBoundary bool) bool {
	inside := func(path string) bool {
		return wiki.IsUnder(path, boundary) && (includeBoundary || path != boundary)
	}
	if marker, found := config.FindScopeRoot(dir, "repo"); found && inside(marker) {
		return true
	}
	for path := dir; inside(path); path = filepath.Dir(path) {
		if _, err := os.Lstat(filepath.Join(path, ".git")); !os.IsNotExist(err) {
			return true
		}
		if path == boundary {
			break
		}
	}
	return false
}

func orientSessionLinks(root, body string) string {
	roots, err := realmshare.LinkedRoots(root)
	if err != nil || len(roots) == 0 {
		return body
	}
	const header = "## Orientation\n"
	var orientation strings.Builder
	orientation.WriteString(header)
	for _, root := range roots {
		fmt.Fprintf(&orientation, "- Linked discovery root: %s\n", root)
	}
	if rest, found := strings.CutPrefix(body, header); found {
		orientation.WriteString(rest)
	} else if body != "" {
		orientation.WriteByte('\n')
		orientation.WriteString(body)
	}
	return strings.TrimRight(orientation.String(), "\n")
}

// preToolUse is a seam so a test can make the hook panic.
var preToolUse = hooks.PreToolUse

// runPreToolUse never fails: a panic or error prints nothing and exits 0, so a
// wiki card can never fail the edit it precedes.
func runPreToolUse(stdin io.Reader) {
	defer func() { _ = recover() }()
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	if out := preToolUse(stdin, home, time.Now().UTC()); out != "" {
		fmt.Println(out)
	}
}

// resolveSessionRoot resolves the session directory through the scope marker
// and Git rather than taking the directory as the root.
func resolveSessionRoot(flagCwd string, stdin io.Reader, repoOverride string) (string, error) {
	dir := sessionStartCwd(flagCwd, stdin, stdinWait)
	if dir == "" {
		var err error
		if dir, err = os.Getwd(); err != nil {
			return "", err
		}
	}
	if repoOverride == "" {
		if home, err := os.UserHomeDir(); err == nil {
			root, _, err := sessionLinkedRealm(dir, filepath.Join(home, ".claude", "CLAUDE.md"))
			if err == nil && root != "" {
				repoOverride = root
			}
		}
	}
	root, _, err := repoctx.ResolveFrom(dir, repoOverride)
	return root, err
}

const (
	stdinWait = time.Second
	cwdHelp   = "directory to resolve the repository from (default: stdin cwd, else process directory)"
)

// pipedStdin returns nil for a terminal or /dev/null, which would otherwise
// wait on or consume typed input.
func pipedStdin(f *os.File) io.Reader {
	fi, err := f.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice != 0 {
		return nil
	}
	return f
}

func sessionStartCwd(flagCwd string, r io.Reader, wait time.Duration) string {
	if flagCwd != "" || r == nil {
		return flagCwd
	}
	input, _ := readSessionStartInput(r, wait, false)
	return input.Cwd
}

type sessionStartInput struct {
	Cwd     string          `json:"cwd"`
	Event   json.RawMessage `json:"hook_event_name"`
	Source  json.RawMessage `json:"source"`
	AgentID json.RawMessage `json:"agent_id"`
}

func (p sessionStartInput) primaryStartup() bool {
	var event, source string
	return json.Unmarshal(p.Event, &event) == nil && event == "SessionStart" &&
		json.Unmarshal(p.Source, &source) == nil && source == "startup"
}

func (p sessionStartInput) primaryAgent() bool {
	return p.AgentID == nil
}

// A harness may leave stdin open; never let it delay startup indefinitely.
func readSessionStartInput(r io.Reader, wait time.Duration, complete bool) (sessionStartInput, error) {
	if r == nil {
		return sessionStartInput{}, fmt.Errorf("missing SessionStart stdin")
	}
	type result struct {
		input sessionStartInput
		err   error
	}
	got := make(chan result, 2)
	go func() {
		var input *sessionStartInput
		limited := &io.LimitedReader{R: r, N: 1 << 20}
		decoder := json.NewDecoder(limited)
		if err := decoder.Decode(&input); err != nil {
			got <- result{err: fmt.Errorf("invalid SessionStart stdin: %w", err)}
			return
		}
		if input == nil {
			got <- result{err: fmt.Errorf("SessionStart stdin must be a JSON object")}
			return
		}
		// Keep cwd available for the baseline even if the stream never closes.
		got <- result{input: *input}
		if complete {
			if _, err := decoder.Token(); err != io.EOF || limited.N == 0 {
				got <- result{err: fmt.Errorf("SessionStart stdin must be one JSON object within 1 MiB")}
				return
			}
			got <- result{}
		}
	}()
	deadline := time.After(wait)
	read := func() result {
		select {
		case next := <-got:
			return next
		case <-deadline:
			return result{err: fmt.Errorf("timed out reading SessionStart stdin")}
		}
	}
	first := read()
	if first.err != nil || !complete {
		return first.input, first.err
	}
	return first.input, read().err
}

// resolveHooksRoot: user scope writes no repository state, so it skips the
// state-root check that refuses an ambiguous repository.
func resolveHooksRoot(scope, override string) (string, error) {
	if scope == "user" {
		return repoctx.ResolveUnchecked(override)
	}
	return repoctx.Resolve(override)
}

// resolveScopeRoot maps "user" to $HOME/.claude and "project" to repoRoot.
func resolveScopeRoot(scope, repoRoot string) (string, error) {
	switch scope {
	case "user":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve scope: get home dir: %w", err)
		}
		return home, nil
	case "project":
		return repoRoot, nil
	default:
		return "", fmt.Errorf("unknown scope %q: must be \"user\" or \"project\"", scope)
	}
}
