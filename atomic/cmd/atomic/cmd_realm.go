package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/prompt"
	"github.com/damusix/atomic-claude/atomic/internal/realmshare"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
	"github.com/spf13/cobra"
)

func buildRealmCmd(repoOverride *string) *cobra.Command {
	parent := &cobra.Command{
		Use:   "realm",
		Short: "Help for sharing realm content",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	parent.AddCommand(buildRealmShareCmd(repoOverride))
	parent.AddCommand(buildRealmSyncCmd(repoOverride))
	parent.AddCommand(buildRealmJoinCmd(repoOverride), buildRealmLinkCmd(repoOverride))
	parent.AddCommand(buildRealmStatusCmd(repoOverride))
	parent.AddCommand(buildRealmUnlinkCmd(repoOverride))
	return parent
}

func buildRealmStatusCmd(repoOverride *string) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Inspect local sharing state and cached remote status",
		Long:  "Inspect the local realm binding, shared paths, and checkout without fetching or changing anything. Remote observations are cached.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := realmCommandRoot(*repoOverride, cmd.Name())
			if err != nil {
				return err
			}
			report, err := realmshare.InspectStatus(cmd.Context(), root)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if jsonOut {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}
			return printRealmStatus(out, report)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit the report as JSON")
	return cmd
}

func printRealmStatus(out io.Writer, report realmshare.StatusReport) error {
	var text strings.Builder
	fmt.Fprintf(&text, "Realm: %q\nRealm ID: %s\nState root: %q\nManaged checkout: %q\nRemote: %q\nBranch: %q\n",
		report.RealmRoot, report.RealmID, report.StateRoot, report.ClonePath, report.Remote, report.Branch)
	head := report.Head
	if head == "" {
		head = "(unborn)"
	}
	fmt.Fprintf(&text, "Local HEAD: %s\n", head)

	fmt.Fprintln(&text, "Cached remote (not queried):")
	if remote := report.CachedRemote; remote == nil {
		fmt.Fprintln(&text, "  No cached observation.")
	} else {
		if remote.Head != "" {
			fmt.Fprintf(&text, "  Observed HEAD: %s\n", remote.Head)
		}
		if remote.DivergenceKnown {
			fmt.Fprintf(&text, "  Ahead: %d, behind: %d\n", remote.Ahead, remote.Behind)
		} else {
			fmt.Fprintln(&text, "  Ahead/behind: unknown")
		}
		if !remote.LastSuccess.IsZero() {
			fmt.Fprintf(&text, "  Last successful sync: %s\n", remote.LastSuccess.UTC().Format(time.RFC3339Nano))
		}
	}

	if len(report.Paths) == 0 {
		fmt.Fprintln(&text, "Shared paths: none")
	} else {
		fmt.Fprintln(&text, "Shared paths:")
		for _, shared := range report.Paths {
			fmt.Fprintf(&text, "  %q (%s) <- %q: %s", shared.Path, shared.Kind, shared.Source, shared.State)
			if shared.Drift {
				fmt.Fprint(&text, " (drift)")
			}
			if shared.Detail != "" {
				fmt.Fprintf(&text, " — %s", shared.Detail)
			}
			fmt.Fprintln(&text)
			if shared.SourceAvailability != "" {
				fmt.Fprintf(&text, "    Clone source: %s", shared.SourceAvailability)
				if shared.SourceDetail != "" {
					fmt.Fprintf(&text, " — %s", strconv.QuoteToGraphic(shared.SourceDetail))
				}
				fmt.Fprintln(&text)
			}
			for _, activation := range shared.Activations {
				printRealmArtifactActivation(&text, activation)
			}
		}
	}
	if len(report.Changes) == 0 {
		fmt.Fprintln(&text, "Local changes: none")
	} else {
		fmt.Fprintln(&text, "Local changes:")
		for _, change := range report.Changes {
			fmt.Fprintf(&text, "  %q: index %q, worktree %q\n", change.Path, change.Index, change.Worktree)
		}
	}
	if len(report.Conflicts) == 0 {
		fmt.Fprintln(&text, "Conflicts: none")
	} else {
		fmt.Fprintln(&text, "Conflicts:")
		for _, conflict := range report.Conflicts {
			if conflict.Path == "" {
				fmt.Fprintf(&text, "  %q (checkout path)\n", conflict.Source)
			} else {
				fmt.Fprintf(&text, "  %q <- %q\n", conflict.Path, conflict.Source)
			}
		}
	}
	_, err := io.WriteString(out, text.String())
	return err
}

func buildRealmSyncCmd(repoOverride *string) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Exchange changes with the shared realm remote",
		Long: "Commit declared changes and exchange commits with the bound remote, even if no local paths are linked.\n" +
			"Sync does not install links. If a share is still pending local completion, rerun that share to finish it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := realmCommandRoot(*repoOverride, cmd.Name())
			if err != nil {
				return err
			}
			result, err := realmshare.Sync(cmd.Context(), root)
			if err != nil {
				return err
			}
			var summary strings.Builder
			if result.Committed {
				summary.WriteString("Committed local shared changes.\n")
			}
			if result.Received {
				summary.WriteString("Received and integrated remote changes.\n")
			}
			if result.Published {
				summary.WriteString("Published changes to the shared realm remote.\n")
			}
			if summary.Len() == 0 {
				summary.WriteString("Shared realm is up to date.\n")
			}
			_, err = io.WriteString(cmd.OutOrStdout(), summary.String())
			return err
		},
	}
}

func buildRealmJoinCmd(repoOverride *string) *cobra.Command {
	return &cobra.Command{
		Use:   "join <git-url>",
		Short: "Download and review a shared realm without linking",
		Long: "Use the remote's advertised default branch when it contains a shared realm manifest. Otherwise, use the only branch with a manifest and refuse to guess if multiple branches qualify. " +
			"Validate the selected branch's shared realm and preview local destinations and collisions without installing links or trusting shared instructions.",
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{"args_hint": "<git-url>"},
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := realmCommandRoot(*repoOverride, cmd.Name())
			if err != nil {
				return err
			}
			result, err := realmshare.Join(cmd.Context(), root, args[0])
			if err != nil {
				return err
			}
			var preview strings.Builder
			fmt.Fprintf(&preview, "Shared realm bound to %q (branch %q)\n", result.Binding.Remote, result.Binding.Branch)
			for _, candidate := range result.Mappings {
				mapping := candidate.Mapping
				fmt.Fprintf(&preview, "  %q (%s) <- %q", mapping.Destination, mapping.Kind, mapping.Source)
				if candidate.Collision {
					fmt.Fprint(&preview, " (local destination occupied)")
				}
				fmt.Fprintln(&preview)
			}
			fmt.Fprintln(&preview, "No links installed. Use atomic realm link to review and approve selected paths.")
			_, err = io.WriteString(cmd.OutOrStdout(), preview.String())
			return err
		},
	}
}

func buildRealmLinkCmd(repoOverride *string) *cobra.Command {
	var review realmReview
	cmd := &cobra.Command{
		Use:   "link [<path>...]",
		Short: "Review and link selected shared realm paths",
		Long: "Review selected realm-relative destinations and explicitly accept trust before installing their symlinks.\n" +
			"In a terminal, omit paths to choose from an unchecked checklist.\n" +
			"Use -- before paths that start with a dash.\n" +
			"To apply without a prompt, preview explicit paths with --dry-run, then rerun with --yes --review <digest> after approval.",
		Annotations: map[string]string{"args_hint": "[<path>...]"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRealmLink(cmd, *repoOverride, args, review)
		},
	}
	cmd.Flags().BoolVar(&review.dryRun, "dry-run", false, "preview selected links and trust without changing anything")
	cmd.Flags().BoolVar(&review.yes, "yes", false, "accept trust and approve linking only explicitly supplied paths; requires --review")
	cmd.Flags().StringVar(&review.digest, "review", "", "approved digest from --dry-run for the same inputs; requires --yes")
	return cmd
}

func runRealmLink(cmd *cobra.Command, repoOverride string, paths []string, review realmReview) error {
	interactive := !review.yes && prompt.IsInteractive()
	if err := review.validate(cmd, paths, interactive); err != nil {
		return err
	}
	root, err := realmCommandRoot(repoOverride, cmd.Name())
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		paths, err = promptRealmLinkPaths(cmd, root)
		if err != nil {
			return err
		}
	}
	plan, err := realmshare.PreviewLink(cmd.Context(), root, paths)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if err := printRealmLinkPlan(out, plan); err != nil {
		return err
	}
	if err := review.check(cmd, plan.ReviewDigest()); err != nil || review.dryRun {
		return err
	}
	if !review.yes {
		approved, err := prompt.Confirm("Trust and link this selection?", "Trust the reviewed shared definition and instruction content for these paths.", false)
		if err != nil {
			return err
		}
		if !approved {
			return errors.New("link not approved; no local paths changed")
		}
	}
	if err := realmshare.ApplyLink(cmd.Context(), plan); err != nil {
		return err
	}
	var result strings.Builder
	for _, selection := range plan.Selections {
		if selection.AlreadyLinked {
			fmt.Fprintf(&result, "Kept existing owned symlink %q.\n", selection.Mapping.Destination)
		} else {
			fmt.Fprintf(&result, "Linked %q.\n", selection.Mapping.Destination)
		}
	}
	_, err = io.WriteString(out, result.String())
	return err
}

func promptRealmLinkPaths(cmd *cobra.Command, root string) ([]string, error) {
	candidates, err := realmshare.LinkCandidates(root)
	if err != nil {
		return nil, err
	}
	choices := make([]huh.Option[string], 0, len(candidates))
	for _, candidate := range candidates {
		dest := candidate.Mapping.Destination
		if !candidate.Collision {
			label := strconv.Quote(dest)
			if candidate.AlreadyLinked {
				label += " (already linked)"
			}
			choices = append(choices, huh.NewOption(label, dest))
		}
	}
	if len(choices) == 0 {
		return nil, errors.New("no eligible shared paths to link; review the join preview for occupied destinations")
	}
	var paths []string
	field := huh.NewMultiSelect[string]().Title("Paths to link").
		Description("Select paths to review; occupied private destinations are not selectable.").
		Options(choices...).Height(8).Value(&paths).Validate(func(paths []string) error {
		if len(paths) == 0 {
			return errors.New("select one or more paths")
		}
		return nil
	})
	if err := huh.NewForm(huh.NewGroup(field)).RunWithContext(cmd.Context()); err != nil {
		return nil, err
	}
	return paths, nil
}

func printRealmLinkPlan(out io.Writer, plan realmshare.LinkPlan) error {
	var preview strings.Builder
	fmt.Fprintln(&preview, "Link preview (requires trust in the reviewed shared definition):")
	fmt.Fprintf(&preview, "  Remote: %q\n  Branch: %q\n", plan.Remote, plan.Branch)
	fmt.Fprintln(&preview, "  Selected realm-local discovery roots:")
	hasArtifacts := false
	for _, selection := range plan.Selections {
		mapping := selection.Mapping
		fmt.Fprintf(&preview, "  %q (%s) <- %q\n", mapping.Destination, mapping.Kind, mapping.Source)
		switch {
		case selection.AlreadyLinked:
			fmt.Fprintln(&preview, "    Keep the existing owned symlink.")
		case selection.RestoresOriginal:
			fmt.Fprintln(&preview, "    Retain the restored original as a backup and install a symlink.")
		default:
			fmt.Fprintln(&preview, "    Install a local symlink.")
		}
		switch mapping.Kind {
		case realmshare.KindFile, realmshare.KindInstructions, realmshare.KindCommand, realmshare.KindAgent:
			fmt.Fprintf(&preview, "    If Claude Code refuses to edit the file symlink, edit the checked-out file %q instead.\n",
				filepath.Join(plan.ClonePath, filepath.FromSlash(mapping.Source)))
		}
		for _, bucket := range selection.Buckets {
			fmt.Fprintf(&preview, "    Shared bucket %q at %q\n", bucket.Name, bucket.Path)
		}
		if mapping.Kind == realmshare.KindInstructions {
			fmt.Fprintln(&preview, "    Shared instructions to review:")
			for line := range strings.SplitSeq(selection.TeamText, "\n") {
				fmt.Fprintf(&preview, "      %s\n", strconv.QuoteToGraphic(line))
			}
		}
		if selection.Artifact != nil {
			hasArtifacts = true
			printRealmArtifactReview(&preview, selection.Artifact)
		}
	}
	if hasArtifacts {
		printRealmArtifactLimits(&preview)
	}
	if len(plan.InstructionNotes) > 0 {
		fmt.Fprintln(&preview, "  Local instruction setup:")
		for _, note := range plan.InstructionNotes {
			fmt.Fprintf(&preview, "    %s\n", note)
		}
	}
	if plan.ClonePath != "" {
		fmt.Fprintf(&preview, "  Shared write access: the managed checkout is %q.\n", plan.ClonePath)
		fmt.Fprintln(&preview, "    If your harness blocks writes outside the local realm, add this checkout to your approved local writable directories.")
		fmt.Fprintln(&preview, "    Claude Code: permissions.additionalDirectories. Codex (workspace-write): sandbox_workspace_write.writable_roots.")
		fmt.Fprintln(&preview, "    Start a new session after saving local settings. Atomic does not change permissions.")
	}
	_, err := io.WriteString(out, preview.String())
	return err
}

func printRealmArtifactReview(out *strings.Builder, review *realmshare.ArtifactReview) {
	fmt.Fprintf(out, "    Artifact source %q (quoted preview):\n", review.SourceFile)
	for line := range strings.SplitSeq(review.Text, "\n") {
		fmt.Fprintf(out, "      %s\n", strconv.QuoteToGraphic(line))
	}
	for _, note := range review.Notes {
		fmt.Fprintf(out, "    %s\n", strconv.QuoteToGraphic(note))
	}
	for _, activation := range review.Native {
		printRealmArtifactActivation(out, activation)
	}
}

func printRealmArtifactLimits(out *strings.Builder) {
	fmt.Fprintln(out, "  Shared artifacts guide an agent's actions when invoked.")
	fmt.Fprintln(out, "  Trust covers updates to selected units from the same remote, not new units or tool permissions.")
	fmt.Fprintln(out, "  Start a new session at the realm root for native discovery. Existing sessions, member repositories, and global profiles are not configured.")
}

func printRealmArtifactActivation(out *strings.Builder, activation realmshare.ArtifactActivation) {
	fmt.Fprintf(out, "    Native %s", activation.Harness)
	if activation.Path != "" {
		fmt.Fprintf(out, " at %q", activation.Path)
	}
	fmt.Fprintf(out, ": %s", activation.State)
	if activation.Version != "" {
		fmt.Fprintf(out, " (tested %q)", activation.Version)
	}
	if activation.Detail != "" {
		fmt.Fprintf(out, " — %s", strconv.QuoteToGraphic(activation.Detail))
	}
	fmt.Fprintln(out)
}

func buildRealmUnlinkCmd(repoOverride *string) *cobra.Command {
	return &cobra.Command{
		Use:   "unlink",
		Short: "Remove owned realm links and restore originals",
		Long:  "Remove only owned realm symlinks and restore retained originals. Keep the binding, managed checkout, and shared changes.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := realmCommandRoot(*repoOverride, cmd.Name())
			if err != nil {
				return err
			}
			if err := realmshare.Unlink(cmd.Context(), root); err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "No owned realm links remain. The shared checkout and binding are retained.")
			return err
		},
	}
}

func buildRealmShareCmd(repoOverride *string) *cobra.Command {
	var options realmshare.ShareOptions
	var review realmReview
	cmd := &cobra.Command{
		Use:   "share [<path>...]",
		Short: "Publish selected realm paths",
		Long: "Review selected realm-relative paths, publish their snapshot, then keep recoverable originals and replace them with symlinks.\n" +
			"The first share requires an existing empty Git remote. Later shares reuse the recorded remote and branch.\n" +
			"In a terminal, omit paths to open the checklist or enter realm-relative paths.\n" +
			"Use -- before paths that start with a dash.\n" +
			"To apply without a prompt, preview explicit paths with --dry-run, then rerun with --yes --review <digest> after approval. Include --remote for the first share.\n" +
			"If publication succeeds but local linking is interrupted, rerun share with the same paths to resume; repeat any remote and branch flags. For approval without a prompt, preview the remaining work for a fresh digest.",
		Annotations: map[string]string{"args_hint": "[<path>...]"},
		RunE: func(cmd *cobra.Command, args []string) error {
			input := options
			input.Paths = args
			return runRealmShare(cmd, *repoOverride, input, review)
		},
	}
	cmd.Flags().StringVar(&options.Remote, "remote", "", "existing empty Git remote for the first share")
	cmd.Flags().StringVar(&options.Branch, "branch", "", "branch to publish (first share defaults to main)")
	cmd.Flags().BoolVar(&review.dryRun, "dry-run", false, "preview publication or pending local cutover without changing anything")
	cmd.Flags().BoolVar(&review.yes, "yes", false, "approve sharing or completing local cutover for explicitly supplied paths; requires --review")
	cmd.Flags().StringVar(&review.digest, "review", "", "approved digest from --dry-run for the same inputs; requires --yes")
	return cmd
}

func runRealmShare(cmd *cobra.Command, repoOverride string, options realmshare.ShareOptions, review realmReview) error {
	interactive := !review.yes && prompt.IsInteractive()
	if err := review.validate(cmd, options.Paths, interactive); err != nil {
		return err
	}

	root, err := realmCommandRoot(repoOverride, cmd.Name())
	if err != nil {
		return err
	}
	needsPrompt := interactive && (options.Remote == "" || options.Branch == "" || len(options.Paths) == 0)
	needsRemote := review.yes && options.Remote == ""
	if needsPrompt || needsRemote {
		binding, err := realmshare.LoadBinding(root)
		initial := errors.Is(err, os.ErrNotExist)
		if err != nil && !initial {
			return err
		}
		if initial && needsRemote {
			return errors.New("the first share with --yes requires --remote pointing to an existing empty Git repository")
		}
		if needsPrompt {
			if err := promptRealmShareInputs(cmd, root, binding, &options, initial); err != nil {
				return err
			}
		}
	}

	plan, err := realmshare.PreviewShare(cmd.Context(), root, options)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if err := printRealmSharePlan(out, plan); err != nil {
		return err
	}
	if err := review.check(cmd, plan.ReviewDigest()); err != nil || review.dryRun {
		return err
	}
	if !review.yes {
		title := "Publish this selection?"
		description := "A successful push is required before any local paths are replaced."
		cancelled := "Publication cancelled."
		if plan.Published {
			title = "Complete local cutover?"
			description = "No push will be attempted."
			cancelled = "Local cutover cancelled."
		}
		approved, err := prompt.Confirm(title, description, false)
		if err != nil {
			return err
		}
		if !approved {
			_, err := fmt.Fprintln(out, cancelled)
			return err
		}
	}
	if err := realmshare.ApplyShare(cmd.Context(), plan); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "Realm share finished.")
	return err
}

type realmReview struct {
	dryRun bool
	yes    bool
	digest string
}

func (r realmReview) validate(cmd *cobra.Command, paths []string, interactive bool) error {
	reviewSet := cmd.Flags().Changed("review")
	switch {
	case r.dryRun && cmd.Flags().Changed("yes"):
		return errors.New("--dry-run and --yes are mutually exclusive")
	case r.dryRun && reviewSet:
		return errors.New("--review cannot be used with --dry-run")
	case reviewSet && !r.yes:
		return errors.New("--review requires --yes")
	case len(paths) == 0 && !interactive:
		return errors.New("supply one or more explicit realm-relative path arguments")
	case r.yes && r.digest == "":
		return errors.New("--yes requires --review <digest> from an approved --dry-run with the same inputs")
	case !interactive && !r.dryRun && !r.yes:
		return fmt.Errorf("%s requires approval: preview with --dry-run, then use --yes --review <digest> with the approved paths", cmd.Name())
	}
	return nil
}

func (r realmReview) check(cmd *cobra.Command, digest string) error {
	if r.dryRun {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Review digest: %s\n", digest)
		return err
	}
	if r.yes && r.digest != digest {
		return fmt.Errorf("review digest does not match the current plan; rerun %s with --dry-run and approve the new preview", cmd.CommandPath())
	}
	return nil
}

func realmCommandRoot(repoOverride, verb string) (string, error) {
	cwd, err := scopeCommandDir(repoOverride)
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}
	claudeMDPath := filepath.Join(home, ".claude", "CLAUDE.md")
	root, insideLink, err := localRealmFromLink(cwd, claudeMDPath)
	if err != nil {
		return "", err
	}
	if insideLink {
		if root == "" {
			return "", fmt.Errorf("%q is inside a symlink not owned by the local realm; run %s from the realm root or specify it with --repo", cwd, verb)
		}
		return root, nil
	}
	realm, found, err := wiki.FindRealm(cwd, claudeMDPath)
	if err != nil {
		return "", err
	}
	if !found {
		if repoOverride != "" {
			return "", fmt.Errorf("no local realm found for --repo %q", repoOverride)
		}
		return "", fmt.Errorf("no local realm found; run %s from a realm directory", verb)
	}
	return realm.Root, nil
}

func promptRealmShareInputs(cmd *cobra.Command, root string, binding realmshare.Binding, options *realmshare.ShareOptions, initial bool) error {
	var fields []huh.Field
	var paths string
	var picked []string
	pickPaths := len(options.Paths) == 0
	if initial && options.Remote == "" {
		fields = append(fields, huh.NewInput().Title("Git remote").
			Description("URL of an existing empty repository. Use Git's configured credentials.").
			Value(&options.Remote).Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return errors.New("enter a Git remote")
			}
			return nil
		}))
	}
	if initial && options.Branch == "" {
		fields = append(fields, huh.NewInput().Title("Branch").Placeholder("main").
			Description("Leave blank to publish to main.").Value(&options.Branch))
	}
	if pickPaths {
		candidates, err := listRealmShareCandidates(root, binding)
		if err != nil {
			return err
		}
		if len(candidates) > 0 {
			choices := make([]huh.Option[string], 0, len(candidates))
			for _, candidate := range candidates {
				choices = append(choices, huh.NewOption(candidate.Label, candidate.Path))
			}
			fields = append(fields, huh.NewMultiSelect[string]().Title("Paths to share").
				Description("Select individual paths, or continue to enter paths manually.").
				Options(choices...).Height(8).Value(&picked))
		}
		fields = append(fields, huh.NewInput().Title("Enter paths manually").
			Description("Comma-separated realm-relative paths. Leave blank to use only the checklist selection.").
			Value(&paths).Validate(func(s string) error {
			if strings.TrimSpace(s) == "" && len(picked) > 0 {
				return nil
			}
			for _, path := range strings.Split(s, ",") {
				if strings.TrimSpace(path) == "" {
					return errors.New("enter one or more paths without empty entries")
				}
			}
			return nil
		}))
	}
	if len(fields) == 0 {
		return nil
	}
	if err := huh.NewForm(huh.NewGroup(fields...)).RunWithContext(cmd.Context()); err != nil {
		return err
	}
	options.Paths = append(options.Paths, picked...)
	if strings.TrimSpace(paths) != "" {
		for path := range strings.SplitSeq(paths, ",") {
			options.Paths = append(options.Paths, strings.TrimSpace(path))
		}
	}
	return nil
}

type realmShareCandidate struct {
	Path  string
	Label string
}

func realmShareCandidates(root string) ([]realmShareCandidate, error) {
	binding, err := realmshare.LoadBinding(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return listRealmShareCandidates(root, binding)
}

func listRealmShareCandidates(root string, binding realmshare.Binding) ([]realmShareCandidate, error) {
	realm, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer realm.Close()
	state, err := config.ResolveStateLocation(root)
	if err != nil {
		return nil, err
	}
	localState, _ := filepath.Rel(root, state.Root)
	localState = filepath.ToSlash(localState)

	shared := make(map[string]bool, len(binding.Links))
	blocked := make(map[string]bool)
	blockParents := func(name string) {
		for parent := path.Dir(name); parent != "." && !blocked[parent]; parent = path.Dir(parent) {
			blocked[parent] = true
		}
	}
	for _, link := range binding.Links {
		shared[link.Path] = true
		blockParents(link.Path)
	}

	buckets := make(map[string]bool)
	for _, bucket := range binding.PrivateBuckets {
		buckets[realmShareBucketPath(root, bucket.Path)] = true
	}
	if binding.RealmID == "" {
		info, err := realm.Lstat("wiki")
		if err == nil && info.IsDir() {
			if info, err := realm.Lstat("wiki/index.md"); err == nil && info.Mode().IsRegular() {
				entries, err := wiki.ReadBucketEntries(filepath.Join(root, "wiki", "index.md"))
				if err != nil {
					return nil, err
				}
				for _, bucket := range entries {
					buckets[realmShareBucketPath(root, bucket.Path)] = true
				}
			}
		}
	}

	var candidates []realmShareCandidate
	err = fs.WalkDir(realm.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch name {
		case ".", ".agents", ".agents/skills", ".agents/commands", ".agents/agents":
			return nil
		}
		if shared[name] {
			candidates = append(candidates, realmShareCandidate{name, name + " (already shared)"})
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		mapping, err := realmshare.CheckShareCandidate(realm, name, localState)
		if err != nil {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		unit := mapping.Kind == realmshare.KindSkill
		repo := false
		if entry.IsDir() {
			git, err := realm.Lstat(name + "/.git")
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			repo = err == nil
			if repo && (name != "wiki" || !git.IsDir()) {
				blockParents(name)
				return fs.SkipDir
			}
			if unit {
				info, err := realm.Lstat(name + "/SKILL.md")
				if err != nil || !info.Mode().IsRegular() {
					return fs.SkipDir
				}
			}
		}
		kind := string(mapping.Kind)
		if buckets[name] && mapping.Kind == realmshare.KindDirectory {
			kind = "bucket"
		}
		candidates = append(candidates, realmShareCandidate{name, fmt.Sprintf("%s (%s)", name, kind)})
		if repo || unit {
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(candidates, func(candidate realmShareCandidate) bool {
		return blocked[candidate.Path]
	}), nil
}

func realmShareBucketPath(root, name string) string {
	if filepath.IsAbs(name) {
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return ""
		}
		name = rel
	}
	return filepath.ToSlash(name)
}

func printRealmSharePlan(out io.Writer, plan realmshare.SharePlan) error {
	var preview strings.Builder
	title := "Publication preview"
	if plan.Published {
		title = "Local cutover preview"
	}
	fmt.Fprintf(&preview, "%s\n  Remote: %q\n  Branch: %q\n  Managed clone: %q\n", title, plan.Remote, plan.Branch, plan.ClonePath)
	switch {
	case plan.Published:
		fmt.Fprintln(&preview, "  The snapshot is already published. Retain backups and replace the remaining local paths with symlinks.")
	case len(plan.Selections) == 0:
		fmt.Fprintln(&preview, "  No new paths to publish or link.")
	default:
		fmt.Fprintln(&preview, "  Publish a snapshot, then retain local originals as backups and replace them with symlinks.")
	}
	if len(plan.Members) != 0 {
		fmt.Fprintln(&preview, "  Portable members:")
		for _, member := range plan.Members {
			fmt.Fprintf(&preview, "    %q at %q (excluded: %t)\n", member.Key, member.Path, member.Exclude)
		}
	}
	hasArtifacts := false
	for _, selection := range plan.Selections {
		mapping := selection.Mapping
		if !plan.Published {
			fmt.Fprintf(&preview, "  %q (%s): %d files, %d bytes\n", mapping.Destination, mapping.Kind, selection.Files, selection.Bytes)
		}
		fmt.Fprintf(&preview, "    Local symlink: %q -> %q\n", mapping.Destination, filepath.Join(plan.ClonePath, filepath.FromSlash(mapping.Source)))
		if selection.Artifact != nil {
			hasArtifacts = true
			printRealmArtifactReview(&preview, selection.Artifact)
		}
		if plan.Published {
			continue
		}
		for _, bucket := range selection.Buckets {
			fmt.Fprintf(&preview, "    Register bucket %q at %q\n", bucket.Name, bucket.Path)
		}
		for _, exclusion := range selection.Exclusions {
			fmt.Fprintf(&preview, "    Exclude %q\n", exclusion)
		}
	}
	if hasArtifacts {
		fmt.Fprintln(&preview, "  Publication links only the canonical realm paths; use realm link to review any separate native destinations.")
		printRealmArtifactLimits(&preview)
	}
	_, err := io.WriteString(out, preview.String())
	return err
}
