package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/bus"
	"github.com/damusix/atomic-claude/atomic/internal/gateway"
)

// defaultGatewayAddr is where `atomic bus gateway` listens for /v1/op when
// --addr is not given.
const defaultGatewayAddr = ":8443"

// gatewayKeysPath is the shared key store every gateway process and every
// enroll/revoke invocation reads and writes — see docs/design/
// atomic-bus-network.md, "Deployment": one ~/.atomic volume for keys, roster
// and room logs.
func gatewayKeysPath(home string) string {
	return filepath.Join(home, ".atomic", "gateway", "keys.json")
}

func runBusGateway(args []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus gateway: resolve home dir: %v\n", err)
		os.Exit(2)
	}
	os.Exit(gatewayAction(args, home, os.Stdout, os.Stderr))
}

// gatewayAction implements `atomic bus gateway`: starts the bus daemon beside
// it and serves /v1/op until interrupted. internal/gateway does the protocol
// work (admission, sealing, the daemon start); this is the CLI glue —
// internal/gateway already imports internal/bus, so that wiring cannot live
// in internal/bus/action.go without an import cycle.
func gatewayAction(args []string, home string, out, errOut io.Writer) int {
	const usage = "Usage: atomic bus gateway [--addr <addr>] [--tls-cert <file>] [--tls-key <file>]\n"

	fs := flag.NewFlagSet("bus-gateway", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var addr, certFile, keyFile string
	fs.StringVar(&addr, "addr", defaultGatewayAddr, "listen address for /v1/op")
	fs.StringVar(&certFile, "tls-cert", "", "TLS certificate file (optional; plain HTTP by default)")
	fs.StringVar(&keyFile, "tls-key", "", "TLS key file (optional; plain HTTP by default)")
	if err := fs.Parse(args); err != nil {
		return int(bus.ExitUsage)
	}
	if fs.NArg() > 0 {
		fmt.Fprint(errOut, usage)
		return int(bus.ExitUsage)
	}
	if (certFile == "") != (keyFile == "") {
		fmt.Fprintln(errOut, "atomic bus gateway: --tls-cert and --tls-key must be given together")
		return int(bus.ExitUsage)
	}

	if err := bus.EnsureDirs(home); err != nil {
		fmt.Fprintf(errOut, "atomic bus gateway: %v\n", err)
		return int(bus.ExitHard)
	}

	if err := bus.RecoverSocket(home); err != nil {
		fmt.Fprintf(errOut, "atomic bus gateway: %v\n", err)
		return int(bus.ExitUnreachable)
	}

	unixLn, err := net.Listen("unix", bus.SocketPath(home))
	if err != nil {
		fmt.Fprintf(errOut, "atomic bus gateway: listen: %v\n", err)
		return int(bus.ExitUnreachable)
	}
	defer unixLn.Close()

	httpLn, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(errOut, "atomic bus gateway: listen %s: %v\n", addr, err)
		return int(bus.ExitUnreachable)
	}
	defer httpLn.Close()

	store := gateway.NewStore(gatewayKeysPath(home))
	window := gateway.NewWindow(gateway.AdmissionWindow, time.Now())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(out, "gateway listening on %s\n", httpLn.Addr())
	if err := gateway.Run(ctx, home, unixLn, httpLn, certFile, keyFile, store, window, nil); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(errOut, "atomic bus gateway: %v\n", err)
		return int(bus.ExitHard)
	}
	return int(bus.ExitOK)
}

// gatewayEnrollAction implements `atomic bus gateway enroll <name>`: generate
// a key, persist it, and print a [bus.remotes] TOML block once — there is no
// way to recover the key afterwards, so the operator pastes it now.
func gatewayEnrollAction(args []string, home string, out, errOut io.Writer) int {
	const usage = "Usage: atomic bus gateway enroll [--tls-cert <file>] <name>\n"
	fs := flag.NewFlagSet("bus-gateway-enroll", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var certFile string
	fs.StringVar(&certFile, "tls-cert", "", "the --tls-cert this gateway is (or will be) run with, so the printed host carries the matching scheme")
	if err := fs.Parse(args); err != nil {
		return int(bus.ExitUsage)
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprint(errOut, usage)
		return int(bus.ExitUsage)
	}
	name := fs.Arg(0)

	store := gateway.NewStore(gatewayKeysPath(home))
	rec, err := store.Enroll(name)
	if err != nil {
		fmt.Fprintf(errOut, "atomic bus gateway enroll: %v\n", err)
		return int(bus.ExitHard)
	}

	scheme := "http"
	if certFile != "" {
		scheme = "https"
	}
	fmt.Fprintf(out, "[bus.remotes.%s]\nhost = %q\nkey  = %q\n", name, scheme+"://<this gateway's reachable host:port>", hex.EncodeToString(rec.Key))
	return int(bus.ExitOK)
}

// gatewayRevokeAction implements `atomic bus gateway revoke <name>`: deletes
// the record from the key store directly. A running gateway notices on its
// next Lookup (Store re-reads on an mtime or size change), so nothing
// restarts.
func gatewayRevokeAction(args []string, home string, out, errOut io.Writer) int {
	const usage = "Usage: atomic bus gateway revoke <name>\n"
	fs := flag.NewFlagSet("bus-gateway-revoke", flag.ContinueOnError)
	fs.SetOutput(errOut)
	if err := fs.Parse(args); err != nil {
		return int(bus.ExitUsage)
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprint(errOut, usage)
		return int(bus.ExitUsage)
	}
	name := fs.Arg(0)

	store := gateway.NewStore(gatewayKeysPath(home))
	if err := store.Revoke(name); err != nil {
		fmt.Fprintf(errOut, "atomic bus gateway revoke: %v\n", err)
		return int(bus.ExitHard)
	}

	fmt.Fprintf(out, "revoked %s\n", name)
	return int(bus.ExitOK)
}

func runBusGatewayEnroll(args []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus gateway enroll: resolve home dir: %v\n", err)
		os.Exit(2)
	}
	os.Exit(gatewayEnrollAction(args, home, os.Stdout, os.Stderr))
}

func runBusGatewayRevoke(args []string) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "atomic bus gateway revoke: resolve home dir: %v\n", err)
		os.Exit(2)
	}
	os.Exit(gatewayRevokeAction(args, home, os.Stdout, os.Stderr))
}
