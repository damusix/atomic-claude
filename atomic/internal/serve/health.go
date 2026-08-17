// Realm-health data behind /api/status. It computes no staleness of its own,
// aggregating wiki.Stale and the doctor code-index check instead. Both are
// injectable through HealthOptions seams for test determinism; a nil seam is
// replaced with the production default, so production is always wired.
package serve

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/doctor"
	"github.com/damusix/atomic-claude/atomic/internal/selfupdate"
	"github.com/damusix/atomic-claude/atomic/internal/version"
)

// WikiStaleResult is the parsed wiki staleness output.
type WikiStaleResult struct {
	// StaleRepos lists members with a STALE summary or DRIFT line.
	StaleRepos []string
	// StaleConcerns lists concern files flagged stale.
	StaleConcerns []string
	// StaleBuckets lists buckets flagged stale.
	StaleBuckets []string
	// BucketDiffKeys usually matches StaleBuckets, kept separate so the UI can
	// distinguish the two.
	BucketDiffKeys []string
}

// IndexHealthResult is the parsed code-index health check.
type IndexHealthResult struct {
	// Severity is "PASS", "WARN", or "FAIL".
	Severity string
	// Detail is the doctor check's own line, surfaced verbatim.
	Detail string
	// FreshCount is the number of fresh members.
	FreshCount int
	// StaleMembers names members whose index is at least staleDays old.
	StaleMembers []string
	// NotIndexed names members with no index db.
	NotIndexed []string
}

// WikiStalenessFn is the wiki-staleness seam.
type WikiStalenessFn func(realmRoot string) WikiStaleResult

// IndexHealthFn is the code-index health seam.
type IndexHealthFn func(realmRoot string) IndexHealthResult

// HealthOptions configures the health handler.
type HealthOptions struct {
	// RealmRoot is the root directory being served.
	RealmRoot string

	// IsRealmScope false reports code-index health only, no wiki staleness.
	IsRealmScope bool

	// WikiStalenessSeam nil takes productionWikiStale.
	WikiStalenessSeam WikiStalenessFn

	// IndexHealthSeam nil takes the scope-appropriate production default.
	IndexHealthSeam IndexHealthFn
}

// staleDays is the code-index staleness threshold shared with the doctor check.
const staleDays = 7

// productionWikiStale reshapes the cached wiki.Stale sets. A hard error yields
// empty sets rather than failing the request.
func productionWikiStale(realmRoot string) WikiStaleResult {
	// One walk shared with /api/nav: the two fire together on every page load,
	// and the shell waits on both.
	sets := navStalenessCache.get(realmRoot)

	var result WikiStaleResult

	// sets.Members is keyed by both base name and raw path, so dedupe.
	seen := map[string]bool{}
	for key := range sets.Members {
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

// productionIndexHealthRealm is the realm-scope IndexHealthFn.
func productionIndexHealthRealm(realmRoot string) IndexHealthResult {
	r := doctor.RunCheckCodeIndexRealmWith(realmRoot, staleDays)
	return parseIndexResult(r)
}

// productionIndexHealthRepo is the repo-scope IndexHealthFn.
func productionIndexHealthRepo(realmRoot string) IndexHealthResult {
	r := doctor.RunCheckCodeIndexWith(realmRoot, staleDays)
	return parseIndexResult(r)
}

// parseIndexResult converts a doctor.Result to an IndexHealthResult. doctor
// returns only a severity and a prose Detail, so the member names below are
// scraped from checks_code_index.go's exact wording:
//
//	"code index: N fresh; stale: a, b (run atomic code sync); not indexed: c"
//
// A format change silently yields empty slices; Detail is still surfaced
// verbatim, so the page degrades rather than lies.
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
			inner := strings.TrimPrefix(part, "code index:")
			inner = strings.TrimSpace(inner)
			var n int
			fmt.Sscanf(inner, "%d fresh", &n)
			res.FreshCount = n
		case strings.HasPrefix(part, "stale:"):
			inner := strings.TrimPrefix(part, "stale:")
			// Drop the trailing "(run atomic code sync)" hint.
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

type healthData struct {
	IsRealmScope bool
	StaleResult  WikiStaleResult
	IndexResult  IndexHealthResult
	// AllFreshWiki is true when no wiki staleness was detected.
	AllFreshWiki bool
}

// resolveHealthSeams fills nil seams with the production defaults, so a caller
// that supplies none still gets real data instead of a nil-seam panic.
func resolveHealthSeams(opts HealthOptions) HealthOptions {
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
	return opts
}

// healthDataFor computes the dashboard data. Seams must already be resolved.
func healthDataFor(opts HealthOptions) healthData {
	var staleResult WikiStaleResult
	if opts.IsRealmScope {
		staleResult = opts.WikiStalenessSeam(opts.RealmRoot)
	}

	indexResult := opts.IndexHealthSeam(opts.RealmRoot)

	allFreshWiki := len(staleResult.StaleRepos) == 0 &&
		len(staleResult.StaleConcerns) == 0 &&
		len(staleResult.BucketDiffKeys) == 0

	return healthData{
		IsRealmScope: opts.IsRealmScope,
		StaleResult:  staleResult,
		IndexResult:  indexResult,
		AllFreshWiki: allFreshWiki,
	}
}

// apiWikiStatus is the /api/status "wiki" field.
type apiWikiStatus struct {
	StaleRepos     []string `json:"staleRepos"`
	StaleConcerns  []string `json:"staleConcerns"`
	StaleBuckets   []string `json:"staleBuckets"`
	BucketDiffKeys []string `json:"bucketDiffKeys"`
	AllFresh       bool     `json:"allFresh"`
}

// apiIndexStatus is the /api/status "index" field.
type apiIndexStatus struct {
	Severity     string   `json:"severity"`
	Detail       string   `json:"detail"`
	FreshCount   int      `json:"freshCount"`
	StaleMembers []string `json:"staleMembers"`
	NotIndexed   []string `json:"notIndexed"`
}

// apiStatusResponse is the /api/status payload.
type apiStatusResponse struct {
	// RunID identifies this process, so the browser does not trust a graph
	// layout warmed by a previous run.
	RunID string `json:"runId"`
	// Build identity, so a bug report can name the binary.
	Version string `json:"version"`
	Commit  string `json:"commit"`
	// LatestVersion is whatever the background update check last recorded.
	// Read, never triggered — serve does no network I/O on a page load.
	LatestVersion   string         `json:"latestVersion"`
	UpdateAvailable bool           `json:"updateAvailable"`
	UptimeSeconds   int64          `json:"uptimeSeconds"`
	IsRealmScope    bool           `json:"isRealmScope"`
	Wiki            apiWikiStatus  `json:"wiki"`
	Index           apiIndexStatus `json:"index"`
}

// nonNilStrings keeps array fields encoding as [] rather than null.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// latestKnownVersion reads the newest release the background update check has
// recorded. Deliberately not a check of its own — `atomic update --check` owns
// the network call, and a browser must not be able to trigger one. Nothing
// recorded means no "latest" row, not an error.
func latestKnownVersion() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	latest := selfupdate.LoadState(config.StatePath(home)).Update.LatestVersion
	if latest == "" {
		return "", false
	}
	return latest, selfupdate.IsNewer(version.Version, latest)
}

// NewAPIStatusHandler serves GET /api/status.
func NewAPIStatusHandler(opts HealthOptions) http.Handler {
	opts = resolveHealthSeams(opts)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := healthDataFor(opts)
		latest, updateAvailable := latestKnownVersion()
		writeAPIJSON(w, apiStatusResponse{
			RunID:           runID,
			Version:         version.Version,
			Commit:          version.Commit,
			LatestVersion:   latest,
			UpdateAvailable: updateAvailable,
			UptimeSeconds:   int64(Uptime().Seconds()),
			IsRealmScope:    data.IsRealmScope,
			Wiki: apiWikiStatus{
				StaleRepos:     nonNilStrings(data.StaleResult.StaleRepos),
				StaleConcerns:  nonNilStrings(data.StaleResult.StaleConcerns),
				StaleBuckets:   nonNilStrings(data.StaleResult.StaleBuckets),
				BucketDiffKeys: nonNilStrings(data.StaleResult.BucketDiffKeys),
				AllFresh:       data.AllFreshWiki,
			},
			Index: apiIndexStatus{
				Severity:     data.IndexResult.Severity,
				Detail:       data.IndexResult.Detail,
				FreshCount:   data.IndexResult.FreshCount,
				StaleMembers: nonNilStrings(data.IndexResult.StaleMembers),
				NotIndexed:   nonNilStrings(data.IndexResult.NotIndexed),
			},
		})
	})
}
