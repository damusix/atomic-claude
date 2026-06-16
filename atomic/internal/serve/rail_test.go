package serve_test

// rail_test.go — FE2: right-rail compositing tests.
//
// TDD contract (failing first — FE2 not yet wired):
//
//  1. GET /rail/<relpath> returns HTTP 200 with an HTML fragment containing
//     three OOB <div> targets: #rail-out-content, #rail-in-content, #rail-graph-content.
//     For a page with known outbound links the out-section must mention them.
//     For a page with known backlinks the in-section must mention them.
//
//  2. GET /rail/<traversal> → 404 (path-traversal guard).
//
//  3. GET /rail/<unknown-page> → 404 (page not in graph).
//
//  4. GET /page/<relpath> (htmx fragment) must include OOB loaders targeting
//     #rail-out-content, #rail-in-content, #rail-graph-content, and #breadcrumb-page.
//     These cause the right rail and breadcrumb to update whenever the main pane
//     swaps to a new page.
//
//  5. The shell (GET /) loads the three Cytoscape scripts in the required order
//     (cytoscape.min.js → elk.bundled.js → cytoscape-elk.min.js) so the rail
//     mini-graph renders. This mirrors the /graph page test but targets the shell
//     — FE3 reuses the same scripts for the system-graph toggle.
//
//  6. An orphan page served via /rail/<relpath> must surface an orphan note in
//     the #rail-in-content fragment (no inbound links).

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildRailRealm creates a small realm for rail compositing tests.
//
//	hub.md  → [spoke](spoke.md) + [[leaf]]    (two outbound links)
//	spoke.md → [[hub]]                         (backlink to hub.md)
//	leaf.md  → no outbound links               (backlink from hub.md)
//	orphan.md → no inbound or outbound links   (pure orphan)
func buildRailRealm(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "hub.md"), "# Hub\n\nSee [spoke](spoke.md) and [[leaf]].\n")
	writeFile(t, filepath.Join(root, "spoke.md"), "# Spoke\n\nSee [[hub]].\n")
	writeFile(t, filepath.Join(root, "leaf.md"), "# Leaf\n\nNo outbound links.\n")
	writeFile(t, filepath.Join(root, "orphan.md"), "# Orphan\n\nNo links at all.\n")
	return root
}

// TestRailHandlerReturnsOutAndInFragments verifies that GET /rail/hub.md returns
// an HTML fragment with three OOB targets: #rail-out-content, #rail-in-content,
// #rail-graph-content. The outbound section must contain links to spoke.md and
// leaf.md; the inbound section must contain a reference to spoke.md.
func TestRailHandlerReturnsOutAndInFragments(t *testing.T) {
	root := buildRailRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/rail/hub.md")
	if err != nil {
		t.Fatalf("GET /rail/hub.md: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/rail/hub.md returned %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// All three rail content slots must appear as OOB targets.
	for _, slot := range []string{"rail-out-content", "rail-in-content", "rail-graph-content"} {
		if !strings.Contains(html, slot) {
			t.Errorf("/rail/hub.md response missing OOB target %q", slot)
		}
	}

	// Out-section: hub.md links to spoke.md (markdown) and leaf.md (wikilink).
	// At minimum the resolved target names must appear in the fragment.
	if !strings.Contains(html, "spoke.md") {
		t.Errorf("/rail/hub.md out-section should mention spoke.md (outbound link target)")
	}
	if !strings.Contains(html, "leaf.md") {
		t.Errorf("/rail/hub.md out-section should mention leaf.md (outbound wikilink target)")
	}

	// In-section: spoke.md links to hub.md, so hub.md should list spoke.md as a backlink.
	if !strings.Contains(html, "spoke.md") {
		t.Errorf("/rail/hub.md in-section should mention spoke.md (backlink)")
	}
}

// TestRailHandlerTraversalReturns404 verifies that a path-traversal attempt on
// /rail/ is rejected with 404 instead of reading arbitrary files.
func TestRailHandlerTraversalReturns404(t *testing.T) {
	root := buildRailRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/rail/../../etc/passwd")
	if err != nil {
		t.Fatalf("GET /rail/../../etc/passwd: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("/rail traversal: want 404, got %d", resp.StatusCode)
	}
}

// TestRailHandlerUnknownPageReturns404 verifies that /rail/<page> for a page
// not in the graph returns 404 — so the UI can show a "not found" state cleanly.
func TestRailHandlerUnknownPageReturns404(t *testing.T) {
	root := buildRailRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/rail/does-not-exist.md")
	if err != nil {
		t.Fatalf("GET /rail/does-not-exist.md: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("/rail/does-not-exist.md: want 404, got %d", resp.StatusCode)
	}
}

// TestRailHandlerOrphanPage verifies that /rail/<orphan> surfaces an orphan note
// in the #rail-in-content fragment — the page has no inbound links.
func TestRailHandlerOrphanPage(t *testing.T) {
	root := buildRailRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/rail/orphan.md")
	if err != nil {
		t.Fatalf("GET /rail/orphan.md: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/rail/orphan.md returned %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// The rail-in-content section for an orphan must say "orphan" or "no inbound".
	lc := strings.ToLower(html)
	if !strings.Contains(lc, "orphan") && !strings.Contains(lc, "no inbound") && !strings.Contains(lc, "no backlinks") {
		t.Errorf("/rail/orphan.md in-section should surface orphan status; html: %s", html)
	}
}

// TestPageFragmentEmitsExactlyOneRailLoader verifies that an htmx fragment
// request to /page/<relpath> (HX-Request: true) emits EXACTLY ONE hx-get to
// /rail/<relpath> — not three separate loaders (one per slot).
//
// Why: three loaders caused three identical round-trips per navigation. The
// consolidated design emits one loader inside #rail-graph-content; the single
// /rail/ response populates all three slots via OOB swaps. Exactly one loader
// in the fragment proves no redundant requests are fired.
//
// The fragment must also include:
//   - #breadcrumb-page OOB swap (immediate title update)
//   - #rail-graph-content OOB swap (contains the one loader)
func TestPageFragmentEmitsExactlyOneRailLoader(t *testing.T) {
	root := buildRailRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	req, err := http.NewRequest(http.MethodGet, baseURL+"/page/hub.md", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("HX-Request", "true")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /page/hub.md (htmx): %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/page/hub.md (htmx) returned %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// Breadcrumb OOB swap must appear.
	if !strings.Contains(html, "breadcrumb-page") {
		t.Errorf("/page/hub.md fragment missing OOB target %q;\nhtml: %q",
			"breadcrumb-page", safeSnippet(html, 400))
	}

	// rail-graph-content OOB swap must appear (hosts the one loader).
	if !strings.Contains(html, "rail-graph-content") {
		t.Errorf("/page/hub.md fragment missing OOB target %q;\nhtml: %q",
			"rail-graph-content", safeSnippet(html, 400))
	}

	// EXACTLY ONE hx-get to /rail/hub.md — count occurrences.
	const railPath = `hx-get="/rail/hub.md"`
	count := strings.Count(html, railPath)
	if count != 1 {
		t.Errorf("/page/hub.md fragment must contain exactly 1 %q, got %d;\nhtml: %q",
			railPath, count, safeSnippet(html, 600))
	}

	// #rail-out-content and #rail-in-content must NOT be OOB targets in the
	// fragment — they are populated by the /rail/ response, not the page fragment.
	for _, deadTarget := range []string{
		`id="rail-out-content" hx-swap-oob`,
		`id="rail-in-content" hx-swap-oob`,
	} {
		if strings.Contains(html, deadTarget) {
			t.Errorf("/page/hub.md fragment must not contain separate OOB target %q (three-loader anti-pattern);\nhtml: %q",
				deadTarget, safeSnippet(html, 600))
		}
	}
}

// TestRailGraphContainerCarriesDataRailGraphURL verifies that the #rail-graph-content
// OOB swap returned by GET /rail/<relpath> contains a rail-cy container element
// with a data-rail-graph-url attribute pointing at /graph/data?node=<page>&depth=1.
//
// Why: the shell's htmx.onLoad handler (not an inline <script>) detects this
// attribute and mounts the Cytoscape mini-graph. Inline scripts in OOB innerHTML
// swaps are not reliably executed by htmx 2.
//
// The attribute is data-rail-graph-url (not data-graph-url) to stay distinct
// from the #mode-system button's JS click handler (FE3). FE3 removed the
// data-graph-url attribute from that button — it is now a pure JS click
// listener. The onLoad handler queries only for [data-rail-graph-url], so
// the two selectors cannot collide.
func TestRailGraphContainerCarriesDataRailGraphURL(t *testing.T) {
	root := buildRailRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/rail/hub.md")
	if err != nil {
		t.Fatalf("GET /rail/hub.md: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/rail/hub.md returned %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// The #rail-graph-content OOB must carry data-rail-graph-url (not data-graph-url).
	if !strings.Contains(html, "data-rail-graph-url") {
		t.Errorf("/rail/hub.md response missing data-rail-graph-url attribute — htmx.onLoad handler will not mount mini-graph;\nhtml: %q",
			safeSnippet(html, 600))
	}

	// Regression guard: rail fragment must NOT use the bare data-graph-url attribute —
	// that seam belongs to #mode-system (FE3). Using it would cause the onLoad handler
	// to match the button on body load and attempt to JSON-decode an HTML page.
	if strings.Contains(html, `data-graph-url=`) {
		t.Errorf("/rail/hub.md response must not carry data-graph-url — use data-rail-graph-url to avoid FE3 collision;\nhtml: %q",
			safeSnippet(html, 600))
	}

	// The data-rail-graph-url must reference the correct endpoint.
	if !strings.Contains(html, "/graph/data") {
		t.Errorf("/rail/hub.md response data-rail-graph-url must reference /graph/data;\nhtml: %q",
			safeSnippet(html, 600))
	}

	// The rail response must NOT contain an inline <script> block for the mini-graph —
	// inline scripts in OOB swaps are not reliably executed by htmx 2.
	if strings.Contains(html, "cytoscape({") {
		t.Errorf("/rail/hub.md response must not contain inline cytoscape({ call — use htmx.onLoad in shell instead;\nhtml: %q",
			safeSnippet(html, 600))
	}
}

// TestShellLoadsGraphScriptsInOrder verifies that the root shell (GET /)
// includes the three Cytoscape scripts in the load-bearing order:
//
//  1. cytoscape.min.js
//  2. elk.bundled.js
//  3. cytoscape-elk.min.js
//
// AND that cytoscape.use( appears after all three. This mirrors the /graph page
// test but targets the shell — FE3 (system-graph toggle) reuses these same
// scripts via the shell-loaded infra without adding new <script> tags.
func TestShellLoadsGraphScriptsInOrder(t *testing.T) {
	root := buildRailRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / returned %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// All three scripts must be present in the shell.
	scripts := []string{
		"/static/vendor/cytoscape.min.js",
		"/static/vendor/elk.bundled.js",
		"/static/vendor/cytoscape-elk.min.js",
	}
	for _, s := range scripts {
		if !strings.Contains(html, s) {
			t.Errorf("shell missing graph script %q — FE3 system-graph toggle requires all three in shell", s)
		}
	}

	// Confirm load ORDER by byte position.
	posC := strings.Index(html, scripts[0])
	posE := strings.Index(html, scripts[1])
	posCE := strings.Index(html, scripts[2])
	if !(posC < posE && posE < posCE) {
		t.Errorf("shell script load order violated: cytoscape@%d elk@%d cytoscape-elk@%d — want C < E < CE",
			posC, posE, posCE)
	}

	// cytoscape.use( must appear after cytoscape-elk.min.js reference.
	posUse := strings.Index(html, "cytoscape.use(")
	if posUse == -1 {
		t.Error("shell missing cytoscape.use( call — rail mini-graph will not initialise")
	} else if posUse < posCE {
		t.Errorf("shell: cytoscape.use( at %d appears before cytoscape-elk.min.js at %d — wrong order",
			posUse, posCE)
	}
}

// safeSnippet returns up to n bytes of s for diagnostics.
func safeSnippet(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
