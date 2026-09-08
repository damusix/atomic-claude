package main

import (
	"fmt"
	"os"

	"github.com/damusix/atomic-claude/atomic/internal/bus"
	"github.com/spf13/cobra"
)

func buildBusCmd() *cobra.Command {
	dispatch := func(args []string) { runBus(args) }
	parent := &cobra.Command{
		Use:   "bus",
		Short: "Inter-session messaging over named rooms (join|leave|send|recv|who|rooms|status|serve|start|stop|restart|tail|say|read|halt|resume|prune|close|end|chat|gateway)",
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
		c.Flags().String("host", "", "join on this remote instead of the local daemon (see [bus.remotes])")
	})
	addSub("leave", "Leave a room (default: the session's last-joined room)", "[<room>]", func(c *cobra.Command) {
		c.Flags().String("session", "", "override CLAUDE_CODE_SESSION_ID")
		c.Flags().String("host", "", "leave on this remote (default: the host this room was joined on)")
	})
	addSub("send", "Send a message; text \"-\" reads stdin", "<room> <text>", func(c *cobra.Command) {
		c.Flags().String("to", "", "comma-separated addressee names (omit for FYI)")
		c.Flags().String("reply-to", "", "id of the message being replied to")
		c.Flags().String("session", "", "override CLAUDE_CODE_SESSION_ID")
		c.Flags().String("host", "", "send on this remote (default: the host this room was joined on)")
		c.Flags().Bool("json", false, "emit the full envelope as JSON (captures the id for --reply-to)")
	})
	addSub("recv", "Receive messages; streams JSON envelopes until SIGTERM", "<room>", func(c *cobra.Command) {
		c.Flags().Bool("json", false, "no-op: recv always streams one JSON envelope per line")
		c.Flags().String("session", "", "override CLAUDE_CODE_SESSION_ID")
		c.Flags().String("host", "", "receive from this remote (default: the host this room was joined on)")
	})
	addSub("who", "List a room's members (default: the session's last-joined room)", "[<room>]", func(c *cobra.Command) {
		c.Flags().Bool("json", false, "emit JSON")
		c.Flags().String("host", "", "list this remote's room (default: the host this room was joined on)")
	})
	addSub("rooms", "List every room the daemon knows about", "", func(c *cobra.Command) {
		c.Flags().Bool("json", false, "emit JSON")
		c.Flags().String("host", "", "list this remote's rooms instead of the local daemon's")
	})
	addSub("status", "Report this session's joined rooms and the daemon's state", "", func(c *cobra.Command) {
		c.Flags().Bool("json", false, "emit JSON")
		c.Flags().String("session", "", "override CLAUDE_CODE_SESSION_ID")
		c.Flags().String("host", "", "report this remote's reachability instead of the local daemon")
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
		c.Flags().String("host", "", "read from this remote instead of the local room log")
		c.Flags().String("session", "", "override CLAUDE_CODE_SESSION_ID (used only to resolve --host precedence)")
	})
	addSub("halt", "Stop a room: agent send fails with exit 7 until resume", "<room>", func(c *cobra.Command) {
		c.Flags().String("text", "", "reason broadcast with the halt")
	})
	addSub("resume", "Clear a room's halt flag; restores agent send", "<room>", nil)
	addSub("prune", "Remove stale members (no live subscription, no recent activity) from a room", "[<room>]", func(c *cobra.Command) {
		c.Flags().Bool("json", false, "emit JSON")
	})
	addSub("close", "Publish a closing envelope, evict every member, and drop the room; owner-requested, no session required", "<room>", nil)
	addSub("end", "Evict one member and stop its listener; driven from the serve UI", "<room> <name>", nil)
	addSub("chat", "Interactive client: joins as a human member; @name, /who, /rooms, /halt, /resume, /quit", "<room>", func(c *cobra.Command) {
		c.Flags().String("as", "", "member name to claim (default: $USER)")
		c.Flags().String("session", "", "override CLAUDE_CODE_SESSION_ID")
	})
	parent.AddCommand(buildBusGatewayCmd())
	return parent
}

// buildBusGatewayCmd wires `atomic bus gateway` and its enroll/revoke
// subcommands. Unlike the addSub verbs above, gateway logic lives in
// cmd/atomic rather than internal/bus/action.go: internal/gateway already
// imports internal/bus, so calling it from within internal/bus would cycle.
func buildBusGatewayCmd() *cobra.Command {
	gw := &cobra.Command{
		Use:                "gateway",
		Short:              "Run the network gateway beside the bus daemon; see bus gateway enroll|revoke",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			runBusGateway(args)
			return nil
		},
	}
	gw.Flags().String("addr", defaultGatewayAddr, "listen address for /v1/op")
	gw.Flags().String("tls-cert", "", "TLS certificate file (optional; plain HTTP by default)")
	gw.Flags().String("tls-key", "", "TLS key file (optional; plain HTTP by default)")

	enroll := &cobra.Command{
		Use:                "enroll",
		Short:              "Generate a key for <name> and print a pasteable [bus.remotes] TOML block, once, with the host's scheme matching --tls-cert",
		Annotations:        map[string]string{"args_hint": "<name>"},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			runBusGatewayEnroll(args)
			return nil
		},
	}
	enroll.Flags().String("tls-cert", "", "the --tls-cert this gateway is (or will be) run with, so the printed host carries the matching scheme")
	gw.AddCommand(enroll)
	gw.AddCommand(&cobra.Command{
		Use:                "revoke",
		Short:              "Delete an enrolled machine's key; the gateway notices on its next lookup",
		Annotations:        map[string]string{"args_hint": "<name>"},
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			runBusGatewayRevoke(args)
			return nil
		},
	})
	return gw
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
