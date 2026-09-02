package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRejectCrossOrigin_OriginHeader(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		{"null origin refused", "null", true},
		{"foreign origin refused", "http://evil.example", true},
		{"own origin passes", "http://example.com", false},
		{"absent origin passes", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/bus/say", nil)
			req.Host = "example.com"
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()
			got := rejectCrossOrigin(rec, req)
			if got != tt.want {
				t.Errorf("rejectCrossOrigin() = %v, want %v (status %d)", got, tt.want, rec.Code)
			}
			if got && rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403", rec.Code)
			}
		})
	}
}

func TestRejectCrossOrigin_SecFetchSite(t *testing.T) {
	tests := []struct {
		name string
		site string
		want bool
	}{
		{"cross-site refused", "cross-site", true},
		{"same-origin passes", "same-origin", false},
		{"absent passes", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/bus/say", nil)
			req.Host = "example.com"
			if tt.site != "" {
				req.Header.Set("Sec-Fetch-Site", tt.site)
			}
			rec := httptest.NewRecorder()
			got := rejectCrossOrigin(rec, req)
			if got != tt.want {
				t.Errorf("rejectCrossOrigin() = %v, want %v (status %d)", got, tt.want, rec.Code)
			}
		})
	}
}

// TestAPIBus_CrossOriginRefused proves the guard runs before any side
// effect: an Origin: null POST to a real running daemon must 403 and leave
// no trace in the room's log; the same request with no Origin, or the
// server's own Origin, proceeds normally.
func TestAPIBus_CrossOriginRefused(t *testing.T) {
	home := busTestHome(t)
	startBusDaemon(t, home)
	srv := httptest.NewServer(newBusTestHandler(home, t.TempDir()))
	defer srv.Close()

	joinResp := postBusJSON(t, srv.URL+"/api/bus/join", map[string]string{"room": "exp"})
	joinResp.Body.Close()
	if joinResp.StatusCode != http.StatusOK {
		t.Fatalf("join room=exp: %d, want 200", joinResp.StatusCode)
	}

	sayReq := func(t *testing.T, origin string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/bus/say", strings.NewReader(`{"room":"exp","text":"hi"}`))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST say: %v", err)
		}
		return resp
	}

	nullResp := sayReq(t, "null")
	defer nullResp.Body.Close()
	if nullResp.StatusCode != http.StatusForbidden {
		t.Fatalf("status with Origin: null = %d, want 403", nullResp.StatusCode)
	}

	logResp, err := http.Get(srv.URL + "/api/bus/log?room=exp")
	if err != nil {
		t.Fatalf("GET log: %v", err)
	}
	log := decodeBusResponse[busLogResponse](t, logResp)
	if len(log.Envelopes) != 0 {
		t.Errorf("hub log has %d envelopes, want 0 — the refused POST reached the hub", len(log.Envelopes))
	}

	noOriginResp := sayReq(t, "")
	defer noOriginResp.Body.Close()
	if noOriginResp.StatusCode != http.StatusOK {
		t.Fatalf("status with no Origin = %d, want 200", noOriginResp.StatusCode)
	}

	sameOriginResp := sayReq(t, srv.URL)
	defer sameOriginResp.Body.Close()
	if sameOriginResp.StatusCode != http.StatusOK {
		t.Fatalf("status with own Origin = %d, want 200", sameOriginResp.StatusCode)
	}
}
