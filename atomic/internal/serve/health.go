// health.go — CP6: realm-health front page handler.
//
// NewHealthHandler returns an http.Handler for the /health route that renders
// a realm-health dashboard as an HTML fragment. The fragment is intended to be
// injected into #main-pane by the shell on load (htmx hx-trigger="load").
//
// The dashboard aggregates two existing engines — no new staleness computation:
//
//   - Wiki staleness: wiki.Stale (DRIFT/STALE/STALE-bucket lines) parsed into
//     stale repo/concern/bucket sets.
//   - Code-index health: doctor.RunCheckCodeIndexRealmWith (realm scope) or
//     doctor.RunCheckCodeIndexWith (repo scope) — worst severity, named repos.
//
// Both engines are injectable via HealthOptions seams for test determinism.
// The production defaults (non-nil, calling the real engines) are set by
// NewHealthHandler when a seam is nil, so production is always wired.
package serve

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/wiki"
)

// WikiStaleResult is the structured output from the wiki staleness parse.
type WikiStaleResult struct {
	// StaleRepos lists member paths/names with STALE summary or DRIFT lines.
	StaleRepos []string
	// StaleConcerns lists concern files with STALE concern lines.
	StaleConcerns []string
	// StaleBuckets lists bucket names with STALE bucket lines.
	StaleBuckets []string
	// BucketDiffKeys lists bucket names with a non-empty diff (STALE bucket).
	// Usually the same as StaleBuckets; kept separate for display granularity.
	BucketDiffKeys []string
}

// IndexHealthResult is the structured result from the code-index health check.
type IndexHealthResult struct {
	// Severity is "PASS", "WARN", or "FAIL".
	Severity string
	// Detail is the full detail line from the doctor check.
	Detail string
	// FreshCount is the number of fresh members.
	FreshCount int
	// StaleMembers names members whose index is stale (age ≥ staleDays).
	StaleMembers []string
	// NotIndexed names members with no index db.
	NotIndexed []string
}

// WikiStalenessFn is the injectable seam for wiki staleness.
// Returns a WikiStaleResult for the given realmRoot.
type WikiStalenessFn func(realmRoot string) WikiStaleResult

// IndexHealthFn is the injectable seam for code-index health.
// Returns an IndexHealthResult for the given realmRoot.
type IndexHealthFn func(realmRoot string) IndexHealthResult

// HealthOptions configures the health dashboard handler.
type HealthOptions struct {
	// RealmRoot is the root directory being served.
	RealmRoot string

	// IsRealmScope is true when serving a realm (wiki present).
	// false = repo/member scope: render code-index health only, no wiki staleness.
	IsRealmScope bool

	// WikiStalenessSeam is the injectable wiki staleness function.
	// When nil, the production default (productionWikiStale) is used.
	WikiStalenessSeam WikiStalenessFn

	// IndexHealthSeam is the injectable code-index health function.
	// When nil, the production default (productionIndexHealth) is used.
	IndexHealthSeam IndexHealthFn
}

// staleDays is the code-index staleness threshold shared with the doctor check.
const staleDays = 7

// productionWikiStale is the production WikiStalenessFn.
// It calls wiki.Stale and parses its output into a WikiStaleResult.
// On hard error (StaleCodeError), it returns an empty result — graceful degradation.
func productionWikiStale(realmRoot string) WikiStaleResult {
	var buf strings.Builder
	code, err := wiki.Stale(realmRoot, &buf)
	if err != nil || code == wiki.StaleCodeError {
		return WikiStaleResult{}
	}

	sets := parseStaleLines(buf.String())

	var result WikiStaleResult

	// Deduplicate members: sets.Members is indexed by both base and raw path,
	// so collect unique values by tracking seen keys.
	seen := map[string]bool{}
	for key := range sets.Members {
		// sets.Members has both base and raw-path entries; prefer the base
		// (shorter form) as the display name.  Only add each unique base once.
		base := key
		if !seen[base] {
			seen[base] = true
			result.StaleRepos = append(result.StaleRepos, base)
		}
	}

	for name := range sets.Buckets {
		result.StaleBuckets = append(result.StaleBuckets, name)
		result.BucketDiffKeys = append(result.BucketDiffKeys, name)
	}

	for name := range sets.Concerns {
		result.StaleConcerns = append(result.StaleConcerns, name)
	}

	return result
}

// productionIndexHealthRealm is the production IndexHealthFn for realm scope.
// It calls doctor.RunCheckCodeIndexRealmWith and parses the result.
func productionIndexHealthRealm(realmRoot string) IndexHealthResult {
	r := doctor.RunCheckCodeIndexRealmWith(realmRoot, staleDays)
	return parseIndexResult(r)
}

// productionIndexHealthRepo is the production IndexHealthFn for repo scope.
// It calls doctor.RunCheckCodeIndexWith.
func productionIndexHealthRepo(realmRoot string) IndexHealthResult {
	r := doctor.RunCheckCodeIndexWith(realmRoot, staleDays)
	return parseIndexResult(r)
}

// parseIndexResult converts a doctor.Result to an IndexHealthResult.
// It parses the detail string for named members; detail is the ground truth
// for human display, so we surface it directly.
//
// The doctor package (RunCheckCodeIndexRealmWith / RunCheckCodeIndexWith) returns
// only doctor.Result{Severity, Detail string} — no structured fields for member
// names or counts.  The secondary badge parsing below is deliberately coupled to
// the Detail format produced by checks_code_index.go:
//
//	"code index: N fresh; stale: a, b (run atomic code sync); not indexed: c"
//
// If the doctor Detail format ever changes, the badge parsing may silently return
// empty slices (graceful degradation: Detail is still surfaced verbatim).
func parseIndexResult(r doctor.Result) IndexHealthResult {
	res := IndexHealthResult{
		Severity: string(r.Severity),
		Detail:   r.Detail,
	}
	detail := r.Detail
	for _, part := range strings.Split(detail, ";") {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "code index:") || strings.HasPrefix(part, "code index: "):
			// "code index: N fresh"
			inner := strings.TrimPrefix(part, "code index:")
			inner = strings.TrimSpace(inner)
			var n int
			fmt.Sscanf(inner, "%d fresh", &n)
			res.FreshCount = n
		case strings.HasPrefix(part, "stale:"):
			// "stale: a, b (run atomic code sync)"
			inner := strings.TrimPrefix(part, "stale:")
			// Strip trailing parenthetical.
			if idx := strings.Index(inner, "("); idx != -1 {
				inner = inner[:idx]
			}
			for _, name := range strings.Split(inner, ",") {
				name = strings.TrimSpace(name)
				if name != "" {
					res.StaleMembers = append(res.StaleMembers, name)
				}
			}
		case strings.HasPrefix(part, "not indexed:"):
			// "not indexed: c"
			inner := strings.TrimPrefix(part, "not indexed:")
			for _, name := range strings.Split(inner, ",") {
				name = strings.TrimSpace(name)
				if name != "" {
					res.NotIndexed = append(res.NotIndexed, name)
				}
			}
		}
	}
	return res
}

// healthTmpl is the HTML template for the health dashboard fragment.
var healthTmpl = template.Must(template.New("health").Parse(`
<div class="health-dashboard">
  <h2 class="health-title">Realm Health</h2>

  {{if .IsRealmScope}}
  <section class="health-section">
    <h3>Wiki Staleness</h3>
    {{if .AllFreshWiki}}
    <p class="health-ok">All wiki artifacts are fresh.</p>
    {{else}}
    <ul class="health-list">
      {{range .StaleResult.StaleRepos}}
      <li><span class="badge badge-stale">stale repo</span> {{.}}</li>
      {{end}}
      {{range .StaleResult.StaleConcerns}}
      <li><span class="badge badge-stale">stale concern</span> {{.}}</li>
      {{end}}
      {{range .StaleResult.BucketDiffKeys}}
      <li><span class="badge badge-diff">bucket diff</span> {{.}}</li>
      {{end}}
    </ul>
    {{end}}
  </section>
  {{else}}
  <p class="health-info">No realm wiki — showing repo code-index health only.</p>
  {{end}}

  <section class="health-section">
    <h3>Code Index</h3>
    <p class="health-detail {{if eq .IndexResult.Severity "PASS"}}health-ok{{else}}health-warn{{end}}">
      <span class="badge badge-severity-{{.IndexResult.Severity}}">{{.IndexResult.Severity}}</span>
      {{.IndexResult.Detail}}
    </p>
    {{if .IndexResult.StaleMembers}}
    <ul class="health-list">
      {{range .IndexResult.StaleMembers}}
      <li><span class="badge badge-stale">stale index</span> {{.}}</li>
      {{end}}
    </ul>
    {{end}}
    {{if .IndexResult.NotIndexed}}
    <ul class="health-list">
      {{range .IndexResult.NotIndexed}}
      <li><span class="badge badge-missing">not indexed</span> {{.}}</li>
      {{end}}
    </ul>
    {{end}}
  </section>

  {{if and .IsRealmScope .AllFreshWiki (eq .IndexResult.Severity "PASS")}}
  <p class="health-ok health-all-fresh">All fresh — realm is healthy.</p>
  {{end}}
</div>
`))

// healthData is the template data struct for the health dashboard.
type healthData struct {
	IsRealmScope bool
	StaleResult  WikiStaleResult
	IndexResult  IndexHealthResult
	// AllFreshWiki is true when no wiki staleness was detected.
	AllFreshWiki bool
}

// NewHealthHandler returns an http.Handler for the /health route.
// When HealthOptions seams are nil, the production defaults are wired.
func NewHealthHandler(opts HealthOptions) http.Handler {
	// Wire production defaults when seams are nil — required by spec.
	// This ensures production is never left with an empty nil-seam.
	if opts.IndexHealthSeam == nil {
		if opts.IsRealmScope {
			opts.IndexHealthSeam = productionIndexHealthRealm
		} else {
			opts.IndexHealthSeam = productionIndexHealthRepo
		}
	}
	if opts.WikiStalenessSeam == nil {
		opts.WikiStalenessSeam = productionWikiStale
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		// Collect wiki staleness (only for realm scope).
		var staleResult WikiStaleResult
		if opts.IsRealmScope {
			staleResult = opts.WikiStalenessSeam(opts.RealmRoot)
		}

		// Collect code-index health (always).
		indexResult := opts.IndexHealthSeam(opts.RealmRoot)

		allFreshWiki := len(staleResult.StaleRepos) == 0 &&
			len(staleResult.StaleConcerns) == 0 &&
			len(staleResult.BucketDiffKeys) == 0

		data := healthData{
			IsRealmScope: opts.IsRealmScope,
			StaleResult:  staleResult,
			IndexResult:  indexResult,
			AllFreshWiki: allFreshWiki,
		}

		var sb strings.Builder
		if err := healthTmpl.Execute(&sb, data); err != nil {
			http.Error(w, "health template error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, sb.String())
	})
}
