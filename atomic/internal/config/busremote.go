package config

import (
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// busRemoteKeyBytes is the key length `atomic bus gateway enroll` generates.
const busRemoteKeyBytes = 32

// No '.': enroll prints the name as a bare [bus.remotes.<name>] key, where a
// dot would open a nested table.
var busRemoteNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// ErrBusRemoteExists is returned by AddBusRemote for a taken name without force.
var ErrBusRemoteExists = errors.New("remote already exists")

// ErrBusRemoteNotFound is returned by RemoveBusRemote for an unknown name.
var ErrBusRemoteNotFound = errors.New("no such remote")

// ExpandHome resolves a leading "~" or "~/" against home.
func ExpandHome(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

// ValidateBusRemoteName requires a name usable as `--host <name>` and as a
// bare TOML key.
func ValidateBusRemoteName(name string) error {
	if !busRemoteNameRE.MatchString(name) {
		return fmt.Errorf("name %q: use letters, digits, '_', '-', starting with a letter or digit", name)
	}
	return nil
}

// ValidateBusRemoteHost accepts a bare host[:port] (dialed as https) or an
// http:// or https:// URL.
func ValidateBusRemoteHost(host string) error {
	if host == "" || strings.ContainsAny(host, " \t\n") {
		return fmt.Errorf("host %q: must be non-empty with no spaces", host)
	}
	raw := host
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("host %q: %v", host, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("host %q: scheme must be http or https", host)
	}
	if u.Host == "" {
		return fmt.Errorf("host %q: missing hostname", host)
	}
	return nil
}

// ValidateBusRemoteKey requires the 64 hex characters enroll prints.
func ValidateBusRemoteKey(key string) error {
	b, err := hex.DecodeString(key)
	if err != nil || len(b) != busRemoteKeyBytes {
		return fmt.Errorf("key: must be the %d hex characters `atomic bus gateway enroll` printed", busRemoteKeyBytes*2)
	}
	return nil
}

// ValidateBusRemoteCA accepts empty (system trust store) or a file holding at
// least one PEM certificate, the same test the bus client applies at dial time.
func ValidateBusRemoteCA(ca, home string) error {
	if ca == "" {
		return nil
	}
	data, err := os.ReadFile(ExpandHome(ca, home))
	if err != nil {
		return fmt.Errorf("ca %q: %v", ca, err)
	}
	if !x509.NewCertPool().AppendCertsFromPEM(data) {
		return fmt.Errorf("ca %q: no PEM certificate found", ca)
	}
	return nil
}

// ValidateBusRemote returns the first reason `atomic bus remote add` refuses r.
func ValidateBusRemote(name string, r BusRemote, home string) error {
	if err := ValidateBusRemoteName(name); err != nil {
		return err
	}
	if err := ValidateBusRemoteHost(r.Host); err != nil {
		return err
	}
	if err := ValidateBusRemoteKey(r.Key); err != nil {
		return err
	}
	return ValidateBusRemoteCA(r.CA, home)
}

// AddBusRemote stores r under name in cfg. A taken name is refused unless
// force is set.
func AddBusRemote(cfg *Config, name string, r BusRemote, force bool) error {
	if _, ok := cfg.Bus.Remotes[name]; ok && !force {
		return fmt.Errorf("%q: %w; pass --force to replace it", name, ErrBusRemoteExists)
	}
	if cfg.Bus.Remotes == nil {
		cfg.Bus.Remotes = make(map[string]BusRemote)
	}
	cfg.Bus.Remotes[name] = r
	return nil
}

// RemoveBusRemote deletes name from cfg.
func RemoveBusRemote(cfg *Config, name string) error {
	if _, ok := cfg.Bus.Remotes[name]; !ok {
		return fmt.Errorf("%q: %w", name, ErrBusRemoteNotFound)
	}
	delete(cfg.Bus.Remotes, name)
	return nil
}
