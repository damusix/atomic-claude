package serve_test

// rail_test.go — right-rail data tests against the /api/rail JSON handler.
//
// Contract:
//
//  1. GET /api/rail/<relpath> returns 200 with "out" edges and "in" backlinks
//     reflecting the page's graph position.
//
//  2. GET /api/rail/<traversal> → 404 (path-traversal guard).
//
//  3. GET /api/rail/<unknown-page> → 404 (page not in graph).
//
//  4. An orphan page reports orphan:true (no inbound links).
//
//  5. Frontmatter Properties (source order, scalar vs. JSON-encoded list
//     values, URL detection) round-trip through the "properties" field.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/serve"
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

// apiRailResponseFor fires GET /api/rail/<relpath> and decodes the response.
func apiRailResponseFor(t *testing.T, baseURL, relpath string) (int, struct {
	RelPath    string     `json:"relpath"`
	Orphan     bool       `json:"orphan"`
	Properties []railProp `json:"properties"`
	Out        []struct {
		Target       string `json:"target"`
		ResolvedPath string `json:"resolvedPath"`
	} `json:"out"`
	In []struct {
		Path string `json:"path"`
	} `json:"in"`
}) {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/rail/" + relpath)
	if err != nil {
		t.Fatalf("GET /api/rail/%s: %v", relpath, err)
	}
	defer resp.Body.Close()
	var got struct {
		RelPath    string     `json:"relpath"`
		Orphan     bool       `json:"orphan"`
		Properties []railProp `json:"properties"`
		Out        []struct {
			Target       string `json:"target"`
			ResolvedPath string `json:"resolvedPath"`
		} `json:"out"`
		In []struct {
			Path string `json:"path"`
		} `json:"in"`
	}
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode /api/rail/%s response: %v", relpath, err)
		}
	}
	return resp.StatusCode, got
}

// TestRailHandlerReturnsOutAndInFragments verifies that GET /api/rail/hub.md
// returns outbound edges to spoke.md and leaf.md, and a backlink from spoke.md.
func TestRailHandlerReturnsOutAndInFragments(t *testing.T) {
	root := buildRailRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	code, got := apiRailResponseFor(t, baseURL, "hub.md")
	if code != http.StatusOK {
		t.Fatalf("/api/rail/hub.md returned %d, want 200", code)
	}

	var sawSpoke, sawLeaf bool
	for _, e := range got.Out {
		if e.ResolvedPath == "spoke.md" {
			sawSpoke = true
		}
		if e.ResolvedPath == "leaf.md" {
			sawLeaf = true
		}
	}
	if !sawSpoke {
		t.Errorf("/api/rail/hub.md out edges should include spoke.md; got %+v", got.Out)
	}
	if !sawLeaf {
		t.Errorf("/api/rail/hub.md out edges should include leaf.md (wikilink); got %+v", got.Out)
	}

	var sawSpokeBacklink bool
	for _, b := range got.In {
		if b.Path == "spoke.md" {
			sawSpokeBacklink = true
		}
	}
	if !sawSpokeBacklink {
		t.Errorf("/api/rail/hub.md backlinks should include spoke.md; got %+v", got.In)
	}
}

// TestRailHandlerTraversalReturns404 verifies that a path-traversal attempt on
// /api/rail/ is rejected with 404 instead of reading arbitrary files.
//
// Exercised directly against the handler (httptest.NewRequest), not through a
// live server: a real net/http.ServeMux 301-redirects a request whose raw path
// contains ".." to the pre-cleaned path before any handler runs, and a payload
// deep enough to escape the realm root also cleans away the "/api/rail/" prefix
// itself — landing on the SPA fallback (200), not the handler under test. The
// traversal guard is enforced inside the handler (safeResolve), which is what
// this test targets.
func TestRailHandlerTraversalReturns404(t *testing.T) {
	root := buildRailRealm(t)
	g := serve.BuildLinkGraph(root)
	handler := serve.NewAPIRailHandler(root, g)

	req := httptest.NewRequest(http.MethodGet, "/api/rail/../../etc/passwd", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("/api/rail traversal: want 404, got %d", rr.Code)
	}
}

// TestRailHandlerUnknownPageReturns404 verifies that /api/rail/<page> for a
// page not in the graph returns 404 — so the UI can show a "not found" state.
func TestRailHandlerUnknownPageReturns404(t *testing.T) {
	root := buildRailRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	code, _ := apiRailResponseFor(t, baseURL, "does-not-exist.md")
	if code != http.StatusNotFound {
		t.Errorf("/api/rail/does-not-exist.md: want 404, got %d", code)
	}
}

// TestRailHandlerOrphanPage verifies that /api/rail/<orphan> reports orphan:true
// — the page has no inbound links.
func TestRailHandlerOrphanPage(t *testing.T) {
	root := buildRailRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	code, got := apiRailResponseFor(t, baseURL, "orphan.md")
	if code != http.StatusOK {
		t.Fatalf("/api/rail/orphan.md returned %d, want 200", code)
	}
	if !got.Orphan {
		t.Errorf("/api/rail/orphan.md should report orphan:true; got %+v", got)
	}
}

// ── FE-SC2 frontmatter Properties slot tests ─────────────────────────────────

// buildFrontmatterRealm creates a small realm where one page has frontmatter
// (with scalar and list keys) and another page has none.
func buildFrontmatterRealm(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// page-with-fm.md has frontmatter: repo (scalar), sources (list), generated (scalar).
	writeFile(t, filepath.Join(root, "page-with-fm.md"),
		"---\nrepo: nes\nsources:\n  - a\n  - b\ngenerated: 2026-06-17\n---\n\n# Page\n\nbody\n")
	writeFile(t, filepath.Join(root, "page-no-fm.md"), "# Plain\n\nNo frontmatter.\n")
	return root
}

// TestRailHandlerPropsFragment_WithFrontmatter verifies that GET
// /api/rail/<page> for a page that HAS frontmatter returns "properties"
// populated with the frontmatter keys in source order (repo before sources
// before generated). A list-valued key is JSON-encoded (IsJSON:true).
func TestRailHandlerPropsFragment_WithFrontmatter(t *testing.T) {
	root := buildFrontmatterRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	props := railPropsFor(t, baseURL, "page-with-fm.md")
	if len(props) != 3 {
		t.Fatalf("expected 3 properties, got %d: %+v", len(props), props)
	}

	// Keys must appear in source order: repo, sources, generated.
	wantKeys := []string{"repo", "sources", "generated"}
	for i, want := range wantKeys {
		if props[i].Key != want {
			t.Errorf("property[%d].Key: got %q, want %q (source order); props=%+v", i, props[i].Key, want, props)
		}
	}

	// Scalar value.
	if props[0].Value != "nes" {
		t.Errorf("repo value: got %q, want %q", props[0].Value, "nes")
	}

	// List value: JSON-encoded, not a comma-joined string.
	if !props[1].IsJSON {
		t.Errorf("sources (list) property must have isJSON:true; got %+v", props[1])
	}
	if !strings.Contains(props[1].Value, "a") || !strings.Contains(props[1].Value, "b") {
		t.Errorf("sources JSON value should contain 'a' and 'b'; got %q", props[1].Value)
	}
}

// TestRailHandlerPropsFragment_NoFrontmatter verifies that GET
// /api/rail/<page> for a page with NO frontmatter returns an empty (nil)
// "properties" list.
func TestRailHandlerPropsFragment_NoFrontmatter(t *testing.T) {
	root := buildFrontmatterRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	props := railPropsFor(t, baseURL, "page-no-fm.md")
	if len(props) != 0 {
		t.Errorf("expected no properties for a page with no frontmatter, got %+v", props)
	}
}

// ── Deliverable B: URL-valued frontmatter properties render as clickable links ──

// buildURLPropRealm creates a realm where one page has a `resource` URL property,
// another has a non-URL scalar property, and a third has a URL in a non-resource key.
func buildURLPropRealm(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "with-resource.md"),
		"---\nresource: https://example.com/x\ntitle: My Page\n---\n\n# Page\n\nbody\n")
	writeFile(t, filepath.Join(root, "non-url-prop.md"),
		"---\nauthor: Alice\nversion: 1.2.3\n---\n\n# Plain\n\nbody\n")
	writeFile(t, filepath.Join(root, "other-url.md"),
		"---\nhomepage: https://example.org/y\ndoc: plain text\n---\n\n# Other\n\nbody\n")
	writeFile(t, filepath.Join(root, "xss-check.md"),
		"---\nresource: https://good.example/path?a=1&b=2\ntitle: <script>alert(1)</script>\n---\n\n# XSS\n\nbody\n")
	return root
}

// railProp mirrors the /api/rail "properties" field's propKV shape.
type railProp struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	IsURL  bool   `json:"isURL"`
	IsJSON bool   `json:"isJSON"`
}

// railPropsFor fires /api/rail/<relpath> and returns its "properties" field.
func railPropsFor(t *testing.T, baseURL, relpath string) []railProp {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/rail/" + relpath)
	if err != nil {
		t.Fatalf("GET /api/rail/%s: %v", relpath, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/rail/%s returned %d, want 200", relpath, resp.StatusCode)
	}
	var got struct {
		Properties []railProp `json:"properties"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode /api/rail/%s response: %v", relpath, err)
	}
	return got.Properties
}

// TestRailPropsURLRenderedAsAnchor verifies that a `resource` frontmatter value
// that is an http(s) URL is marked isURL:true in the /api/rail "properties"
// field, while a non-URL scalar (title) is not.
func TestRailPropsURLRenderedAsAnchor(t *testing.T) {
	root := buildURLPropRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	props := railPropsFor(t, baseURL, "with-resource.md")

	var resourceProp, titleProp *railProp
	for i := range props {
		switch props[i].Key {
		case "resource":
			resourceProp = &props[i]
		case "title":
			titleProp = &props[i]
		}
	}
	if resourceProp == nil {
		t.Fatalf("resource property missing; props: %+v", props)
	}
	if !resourceProp.IsURL {
		t.Errorf("resource URL value must have isURL:true; got %+v", resourceProp)
	}
	if resourceProp.Value != "https://example.com/x" {
		t.Errorf("resource value: got %q, want %q", resourceProp.Value, "https://example.com/x")
	}
	if titleProp != nil && titleProp.IsURL {
		t.Errorf("non-URL 'title' value must not have isURL:true; got %+v", titleProp)
	}
}

// TestRailPropsNonURLStaysPlainText verifies that a page with only non-URL
// scalar properties in frontmatter has isURL:false for every property.
func TestRailPropsNonURLStaysPlainText(t *testing.T) {
	root := buildURLPropRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	props := railPropsFor(t, baseURL, "non-url-prop.md")
	if len(props) == 0 {
		t.Fatal("expected at least one property (author/version)")
	}
	for _, p := range props {
		if p.IsURL {
			t.Errorf("non-URL prop %q must not have isURL:true; got %+v", p.Key, p)
		}
	}
}

// TestRailPropsOtherURLKeyRenderedAsAnchor verifies that any property value
// (not just `resource`) that is an http(s) URL is marked isURL:true.
func TestRailPropsOtherURLKeyRenderedAsAnchor(t *testing.T) {
	root := buildURLPropRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	props := railPropsFor(t, baseURL, "other-url.md")

	var homepage, doc *railProp
	for i := range props {
		switch props[i].Key {
		case "homepage":
			homepage = &props[i]
		case "doc":
			doc = &props[i]
		}
	}
	if homepage == nil || !homepage.IsURL {
		t.Errorf("URL-valued 'homepage' prop must have isURL:true; got %+v", homepage)
	}
	if doc != nil && doc.IsURL {
		t.Errorf("non-URL 'doc' prop must not have isURL:true; got %+v", doc)
	}
}

// TestRailPropsURLHTMLEscaped verifies that frontmatter values round-trip
// through the /api/rail JSON response unmangled (the client is responsible
// for HTML-escaping on render; encoding/json's own escaping already prevents
// a raw <script> tag from appearing verbatim in the wire payload).
func TestRailPropsURLHTMLEscaped(t *testing.T) {
	root := buildURLPropRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	resp, err := http.Get(baseURL + "/api/rail/xss-check.md")
	if err != nil {
		t.Fatalf("GET /api/rail/xss-check.md: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/rail/xss-check.md returned %d, want 200", resp.StatusCode)
	}
	rawBody, _ := io.ReadAll(resp.Body)

	// encoding/json's default HTML-escaping (SetEscapeHTML stays enabled per
	// api_handlers.go's writeAPIJSON) means the raw wire bytes never contain
	// an unescaped "<script>" sequence.
	if strings.Contains(string(rawBody), "<script>alert") {
		t.Errorf("raw <script> must not appear unescaped in the JSON wire payload; body: %s", rawBody)
	}

	var got struct {
		Properties []railProp `json:"properties"`
	}
	if err := json.Unmarshal(rawBody, &got); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, rawBody)
	}
	var resourceProp *railProp
	for i := range got.Properties {
		if got.Properties[i].Key == "resource" {
			resourceProp = &got.Properties[i]
		}
	}
	if resourceProp == nil || !resourceProp.IsURL {
		t.Fatalf("resource property missing or not marked isURL; props: %+v", got.Properties)
	}
	if resourceProp.Value != "https://good.example/path?a=1&b=2" {
		t.Errorf("resource value must round-trip unescaped through JSON decode; got %q", resourceProp.Value)
	}
}
