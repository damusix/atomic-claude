package serve_test

// These run against the real server, not a bare handler: route registration and
// the ctx-driven shutdown wiring are what is under test, and an open-ended stream
// needs a real listener rather than a ResponseRecorder.

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// /events and /status are neighbours in the route table; each must keep its own
// content type.
func TestEvents_RouteReachable_NoCollisionWithStatus(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "README.md"), "# Readme\n")

	baseURL, shutdown := startTestServer(t, startOpts(t, dir))
	defer shutdown()
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	statusResp, err := http.Get(baseURL + "/status")
	if err != nil {
		t.Fatalf("GET /status: %v", err)
	}
	defer statusResp.Body.Close()
	if ct := statusResp.Header.Get("Content-Type"); strings.Contains(ct, "text/event-stream") {
		t.Errorf("/status unexpectedly served as an SSE stream: %s", ct)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/events", nil)
	if err != nil {
		t.Fatalf("build /events request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("/events Content-Type = %q, want text/event-stream", ct)
	}

	line, err := readDataLine(bufio.NewReader(resp.Body))
	if err != nil {
		t.Fatalf("read first SSE line: %v", err)
	}
	var payload struct {
		FP      string   `json:"fp"`
		Changed []string `json:"changed"`
	}
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		t.Fatalf("unmarshal resync payload %q: %v", line, err)
	}
	if payload.FP == "" {
		t.Error("resync payload fp must not be empty")
	}
}

// Ctrl-C with a browser tab open must still exit inside the 5s graceful window.
func TestEvents_ShutdownWithLiveSubscriber_CompletesPromptly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "README.md"), "# Readme\n")

	baseURL, shutdown := startTestServer(t, startOpts(t, dir))
	waitReady(t, baseURL+"/healthz", 3*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/events", nil)
	if err != nil {
		t.Fatalf("build /events request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()

	// Keeps the connection live rather than idle, as an open browser tab would.
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		buf := make([]byte, 512)
		for {
			if _, err := resp.Body.Read(buf); err != nil {
				return
			}
		}
	}()

	start := time.Now()
	shutdown() // cancels the server context and asserts exit within 5s
	elapsed := time.Since(start)

	if elapsed >= 4*time.Second {
		t.Errorf("shutdown with a live /events subscriber took %v, want comfortably under the 5s window", elapsed)
	}

	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Error("subscriber's /events connection was never closed after shutdown")
	}
}

// readDataLine returns the JSON payload of the next non-blank SSE data frame.
func readDataLine(br *bufio.Reader) (string, error) {
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return "", err
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")), nil
	}
}
