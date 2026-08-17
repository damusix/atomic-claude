package serve_test

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

// buildRailRealm gives the rail one page of each shape: two outbound links
// (hub), a reciprocal backlink (spoke), inbound only (leaf), neither (orphan).
func buildRailRealm(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "hub.md"), "# Hub\n\nSee [spoke](spoke.md) and [[leaf]].\n")
	writeFile(t, filepath.Join(root, "spoke.md"), "# Spoke\n\nSee [[hub]].\n")
	writeFile(t, filepath.Join(root, "leaf.md"), "# Leaf\n\nNo outbound links.\n")
	writeFile(t, filepath.Join(root, "orphan.md"), "# Orphan\n\nNo links at all.\n")
	return root
}

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

// Driven against the handler rather than a live server: ServeMux 301-redirects a
// ".." path before any handler runs, and a deep enough payload cleans away the
// "/api/rail/" prefix too, landing on the SPA fallback instead of the guard.
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

// buildFrontmatterRealm pairs a page carrying scalar and list frontmatter keys
// with one carrying none.
func buildFrontmatterRealm(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "page-with-fm.md"),
		"---\nrepo: nes\nsources:\n  - a\n  - b\ngenerated: 2026-06-17\n---\n\n# Page\n\nbody\n")
	writeFile(t, filepath.Join(root, "page-no-fm.md"), "# Plain\n\nNo frontmatter.\n")
	return root
}

func TestRailHandlerPropsFragment_WithFrontmatter(t *testing.T) {
	root := buildFrontmatterRealm(t)

	baseURL, shutdown := startTestServer(t, startOpts(t, root))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	props := railPropsFor(t, baseURL, "page-with-fm.md")
	if len(props) != 3 {
		t.Fatalf("expected 3 properties, got %d: %+v", len(props), props)
	}

	wantKeys := []string{"repo", "sources", "generated"}
	for i, want := range wantKeys {
		if props[i].Key != want {
			t.Errorf("property[%d].Key: got %q, want %q (source order); props=%+v", i, props[i].Key, want, props)
		}
	}

	if props[0].Value != "nes" {
		t.Errorf("repo value: got %q, want %q", props[0].Value, "nes")
	}

	// JSON-encoded, not comma-joined.
	if !props[1].IsJSON {
		t.Errorf("sources (list) property must have isJSON:true; got %+v", props[1])
	}
	if !strings.Contains(props[1].Value, "a") || !strings.Contains(props[1].Value, "b") {
		t.Errorf("sources JSON value should contain 'a' and 'b'; got %q", props[1].Value)
	}
}

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

// buildURLPropRealm covers URL detection: a `resource` URL, plain scalars, a URL
// under a non-resource key, and a value carrying markup.
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

// railProp mirrors the server-side propKV wire shape.
type railProp struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	IsURL  bool   `json:"isURL"`
	IsJSON bool   `json:"isJSON"`
}

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

// Detection keys off the value, not the property name.
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

// The client escapes on render; the wire contract is that values round-trip
// unmangled and no raw <script> ever reaches the payload.
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

	// writeAPIJSON leaves SetEscapeHTML on, so the raw bytes cannot carry it.
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
