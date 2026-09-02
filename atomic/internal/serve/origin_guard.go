package serve

import "net/http"

// rejectCrossOrigin replaces the containment the iframe sandbox used to
// provide against POSTs from a bundle mock: sandbox alone no longer blocks
// scripts, so a mock running on this machine can now reach these
// loopback-gated write routes. An opaque-origin document — the case a
// sandboxed frame produces — sends "Origin: null", which this check refuses
// because it can never equal the server's own origin.
func rejectCrossOrigin(w http.ResponseWriter, r *http.Request) bool {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	ownOrigin := scheme + "://" + r.Host

	if origin := r.Header.Get("Origin"); origin != "" && origin != ownOrigin {
		writeAPIError(w, http.StatusForbidden, "cross-origin request refused")
		return true
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		writeAPIError(w, http.StatusForbidden, "cross-origin request refused")
		return true
	}
	return false
}
