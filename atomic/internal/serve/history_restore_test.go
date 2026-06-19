package serve_test

// history_restore_test.go — Back/Forward must not destroy the nav shell.
//
// htmx 4 keeps no localStorage history cache, so every Back/Forward is a fresh GET
// of the pushed URL carrying HX-History-Restore-Request, and htmx replaces the
// whole <body> with the response. Because we use the HX-Request header to return
// bare #main-pane fragments, the server must treat a HX-History-Restore-Request as
// a document load and return the full shell — otherwise a restore wipes the shell.
// This is enforced purely server-side (fragmentRequest); htmx 4 removed the
// historyRestoreAsHxRequest config the v2 shell used as belt-and-suspenders.

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
