package config

import (
	"bytes"
	"encoding/pem"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every config write round-trips the whole file through Config, deleting any
// table Config does not model; the bus reads [bus.remotes] from this file.
func TestConfigSetPreservesBusRemotes(t *testing.T) {
	home := t.TempDir()
	path := TOMLPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := "[bus.remotes.web-api]\nhost = \"https://bus.example.com\"\nkey = \"abcd\"\nca = \"~/ca.pem\"\n"
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	if code := Run([]string{"set", "update.check", "false"}, home, &bytes.Buffer{}, &stderr); code != 0 {
		t.Fatalf("config set exit %d: %s", code, stderr.String())
	}

	cfg, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, w := range warns {
		if strings.Contains(w.Message, "bus") {
			t.Errorf("[bus] should be a known section, got warning %q", w.Message)
		}
	}
	got := cfg.Bus.Remotes["web-api"]
	want := BusRemote{Host: "https://bus.example.com", Key: "abcd", CA: "~/ca.pem"}
	if got != want {
		t.Errorf("bus.remotes.web-api after config set = %+v, want %+v", got, want)
	}
}

func testCAPEM(t *testing.T) []byte {
	t.Helper()
	srv := httptest.NewTLSServer(nil)
	t.Cleanup(srv.Close)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
}

func TestValidateBusRemote(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "ca.pem"), testCAPEM(t), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "junk.pem"), []byte("not a cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("ab", 32)
	cases := []struct {
		name    string
		remote  string
		r       BusRemote
		wantErr bool
	}{
		{"bare host dials https", "web-api", BusRemote{Host: "bus.example.com:7777", Key: key}, false},
		{"http url", "lan", BusRemote{Host: "http://10.0.0.5:7777", Key: key}, false},
		{"ca under home", "priv", BusRemote{Host: "bus.example.com", Key: key, CA: "~/ca.pem"}, false},
		{"name with space breaks --host", "web api", BusRemote{Host: "bus.example.com", Key: key}, true},
		{"dot in name nests the TOML table", "my.box", BusRemote{Host: "bus.example.com", Key: key}, true},
		{"ca without a certificate fails at dial time", "x", BusRemote{Host: "bus.example.com", Key: key, CA: "~/junk.pem"}, true},
		{"empty name", "", BusRemote{Host: "bus.example.com", Key: key}, true},
		{"ftp scheme", "x", BusRemote{Host: "ftp://bus.example.com", Key: key}, true},
		{"scheme without host", "x", BusRemote{Host: "https://", Key: key}, true},
		{"short key", "x", BusRemote{Host: "bus.example.com", Key: "abcd"}, true},
		{"non-hex key", "x", BusRemote{Host: "bus.example.com", Key: strings.Repeat("zz", 32)}, true},
		{"missing ca file", "x", BusRemote{Host: "bus.example.com", Key: key, CA: "~/nope.pem"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateBusRemote(tc.remote, tc.r, home)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateBusRemote(%q, %+v) err = %v, wantErr %v", tc.remote, tc.r, err, tc.wantErr)
			}
		})
	}
}

// Replacing a remote silently would drop a working key, so a taken name needs
// an explicit force.
func TestAddBusRemoteRefusesTakenNameWithoutForce(t *testing.T) {
	cfg := Default()
	first := BusRemote{Host: "a.example.com", Key: "k1"}
	if err := AddBusRemote(cfg, "web", first, false); err != nil {
		t.Fatal(err)
	}
	if err := AddBusRemote(cfg, "web", BusRemote{Host: "b.example.com"}, false); !errors.Is(err, ErrBusRemoteExists) {
		t.Fatalf("second add err = %v, want ErrBusRemoteExists", err)
	}
	if cfg.Bus.Remotes["web"] != first {
		t.Fatalf("refused add changed the entry: %+v", cfg.Bus.Remotes["web"])
	}
	if err := AddBusRemote(cfg, "web", BusRemote{Host: "b.example.com"}, true); err != nil {
		t.Fatalf("forced add: %v", err)
	}
	if cfg.Bus.Remotes["web"].Host != "b.example.com" {
		t.Fatalf("forced add did not replace: %+v", cfg.Bus.Remotes["web"])
	}
}

func TestRemoveBusRemote(t *testing.T) {
	cfg := Default()
	if err := RemoveBusRemote(cfg, "web"); !errors.Is(err, ErrBusRemoteNotFound) {
		t.Fatalf("remove unknown err = %v, want ErrBusRemoteNotFound", err)
	}
	_ = AddBusRemote(cfg, "web", BusRemote{Host: "a"}, false)
	if err := RemoveBusRemote(cfg, "web"); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Bus.Remotes["web"]; ok {
		t.Fatal("remote still present after remove")
	}
}
