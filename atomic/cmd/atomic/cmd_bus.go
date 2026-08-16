package main

import (
	"fmt"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/bus"
	"github.com/spf13/cobra"
)

// buildBusCmd builds the "bus" parent +
// join|leave|send|recv|who|rooms|status|serve|start|stop|restart|tail|say|read|halt|resume|prune|close|chat
// children. Dispatch is runBus (→ bus.BusAction from internal/bus/action.go).
func buildBusCmd() *cobra.Command {
	dispatch := func(args []string) { runBus(args) }
	parent := &cobra.Command{
		Use:   "bus",
		Short: "Inter-session messaging over named rooms (join|leave|send|recv|who|rooms|status|serve|start|stop|restart|tail|say|read|halt|resume|prune|close|chat)",
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
	addSub("join", "Join a room under a name; auto-spawns the daemon", "<room>", func(c *cobra.Command) {
		c.Flags().String("as", "", "member name to claim (default: repo-root basename)")
		c.Flags().String("mode", "participate", "participate or observe")
		c.Flags().String("kind", "agent", "agent or human")
		c.Flags().String("session", "", "override CLAUDE_CODE_SESSION_ID")
	})
	addSub("leave", "Leave a room (default: the session's last-joined room)", "[<room>]", func(c *cobra.Command) {
		c.Flags().String("session", "", "override CLAUDE_CODE_SESSION_ID")
	})
	addSub("send", "Send a message; text \"-\" reads stdin", "<room> <text>", func(c *cobra.Command) {
		c.Flags().String("to", "", "comma-separated addressee names (omit for FYI)")
		c.Flags().String("reply-to", "", "id of the message being replied to")
		c.Flags().String("session", "", "override CLAUDE_CODE_SESSION_ID")
		c.Flags().Bool("json", false, "emit the full envelope as JSON (captures the id for --reply-to)")
	})
	addSub("recv", "Receive messages; streams JSON envelopes until SIGTERM", "<room>", func(c *cobra.Command) {
		c.Flags().Bool("json", false, "no-op: recv always streams one JSON envelope per line")
		c.Flags().String("session", "", "override CLAUDE_CODE_SESSION_ID")
	})
	addSub("who", "List a room's members (default: the session's last-joined room)", "[<room>]", func(c *cobra.Command) {
		c.Flags().Bool("json", false, "emit JSON")
	})
	addSub("rooms", "List every room the daemon knows about", "", func(c *cobra.Command) {
		c.Flags().Bool("json", false, "emit JSON")
	})
	addSub("status", "Report this session's joined rooms and the daemon's state", "", func(c *cobra.Command) {
		c.Flags().Bool("json", false, "emit JSON")
		c.Flags().String("session", "", "override CLAUDE_CODE_SESSION_ID")
	})
	addSub("serve", "Run the daemon in the foreground; stopped via bus stop", "", nil)
	addSub("start", "Spawn the daemon if none is listening; idempotent", "", nil)
	addSub("stop", "Stop a running daemon; exit 0 if none is running", "", nil)
	addSub("restart", "Stop then start the daemon; the version-skew remedy", "", nil)
	addSub("tail", "Watch a room's traffic without joining; never appears in who", "[<room>]", func(c *cobra.Command) {
		c.Flags().Bool("all-rooms", false, "interleave every room, prefixed per line")
		c.Flags().Bool("json", false, "emit JSONL instead of rendered lines")
		c.Flags().Bool("only-addressed", false, "show only messages with an explicit addressee")
		c.Flags().String("from", "", "show only messages from this sender")
	})
	addSub("say", "Send a one-shot human message without joining; always passes, even halted", "<room> <text>", func(c *cobra.Command) {
		c.Flags().String("to", "", "comma-separated addressee names (omit for FYI)")
	})
	addSub("read", "Print one message's full text from the room log; no daemon needed", "<room> <msg-id>", func(c *cobra.Command) {
		c.Flags().Bool("json", false, "emit the raw envelope JSON")
	})
	addSub("halt", "Stop a room: agent send fails with exit 7 until resume", "<room>", func(c *cobra.Command) {
		c.Flags().String("text", "", "reason broadcast with the halt")
	})
	addSub("resume", "Clear a room's halt flag; restores agent send", "<room>", nil)
	addSub("prune", "Remove stale members (no live subscription, no recent activity) from a room", "[<room>]", func(c *cobra.Command) {
		c.Flags().Bool("json", false, "emit JSON")
	})
	addSub("close", "Publish a closing envelope, evict every member, and drop the room; owner-requested, no session required", "<room>", nil)
	addSub("chat", "Interactive client: joins as a human member; @name, /who, /rooms, /halt, /resume, /quit", "<room>", func(c *cobra.Command) {
		c.Flags().String("as", "", "member name to claim (default: $USER)")
		c.Flags().String("session", "", "override CLAUDE_CODE_SESSION_ID")
	})
	return parent
}

func runBus(args []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus: resolve home dir: %v\n", err)
		os.Exit(2)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus: resolve cwd: %v\n", err)
		os.Exit(2)
	}

	os.Exit(bus.BusAction(args, home, cwd, os.Stdout))
}
