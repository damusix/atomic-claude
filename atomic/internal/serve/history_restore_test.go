package serve_test

// history_restore_test.go — Back/Forward must not destroy the nav shell.
//
// On an htmx history cache miss, htmx re-requests the pushed URL and replaces the
// whole <body> with the response. Because we use the HX-Request header to return
// bare #main-pane fragments, a restore that also carried HX-Request would get a
// fragment and wipe the shell. The server treats a HX-History-Restore-Request as
// a document load (returns the full shell); the shell also sets
// htmx.config.historyRestoreAsHxRequest=false so htmx omits HX-Request on restore.

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestHistoryRestore_ReturnsShellNotFragment(t *testing.T) {
	root := buildPageHierarchyRealm(t)
	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	// Simulate htmx's history cache-miss restore: HX-Request + the restore header.
	req, err := http.NewRequest(http.MethodGet, baseURL+"/page/docs/reference/serve.md", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("HX-Request", "true")
	req.Header.Set("HX-History-Restore-Request", "true")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("restore GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// A restore must return the FULL shell document (htmx replaces <body> with it),
	// NOT a bare fragment — otherwise Back/Forward destroys the nav shell.
	if !strings.Contains(html, "<!DOCTYPE") {
		t.Errorf("history restore must return a full shell document, got a fragment:\n%s", html)
	}
	for _, landmark := range shellLandmarks {
		if !strings.Contains(html, landmark) {
			t.Errorf("history restore missing shell landmark %q", landmark)
		}
	}
	// The restored shell must re-load the requested page into #main-pane.
	if !strings.Contains(html, `hx-get="/page/docs/reference/serve.md"`) {
		t.Errorf("restored shell must boot the requested page into #main-pane; html:\n%s", html)
	}
}

func TestShell_DisablesHistoryRestoreAsHxRequest(t *testing.T) {
	root := buildPageHierarchyRealm(t)
	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// The htmx-config meta must disable historyRestoreAsHxRequest so htmx omits
	// HX-Request on restore (belt with the server's HX-History-Restore-Request
	// suspenders).
	if !strings.Contains(html, "historyRestoreAsHxRequest") {
		t.Errorf("shell must set htmx historyRestoreAsHxRequest=false; html head missing the config")
	}
}
