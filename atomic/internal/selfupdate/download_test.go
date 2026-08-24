package selfupdate

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type progressCall struct {
	received int64
	total    int64
}

func TestDownloadEmitsThrottledProgressAndFinalTotal(t *testing.T) {
	payload := make([]byte, 2*1024*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		w.Write(payload)
	}))
	defer srv.Close()

	var calls []progressCall
	c := &Client{}
	dst := filepath.Join(t.TempDir(), "asset")
	err := c.download(context.Background(), srv.URL, dst, func(received, total int64) {
		calls = append(calls, progressCall{received, total})
	})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if len(calls) < 2 {
		t.Fatalf("expected throttled progress plus final emit, got %d calls", len(calls))
	}
	for i := 1; i < len(calls); i++ {
		if calls[i].received < calls[i-1].received {
			t.Fatalf("received went backwards at call %d: %+v", i, calls)
		}
	}
	last := calls[len(calls)-1]
	if last.received != int64(len(payload)) || last.total != int64(len(payload)) {
		t.Fatalf("final emit should report the full size, got %+v", last)
	}
}

func TestDownloadUnknownLengthFinalEmitReportsReceivedAsTotal(t *testing.T) {
	chunk := make([]byte, 600*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(chunk)
		w.(http.Flusher).Flush()
		w.Write(chunk)
	}))
	defer srv.Close()

	var calls []progressCall
	c := &Client{}
	dst := filepath.Join(t.TempDir(), "asset")
	err := c.download(context.Background(), srv.URL, dst, func(received, total int64) {
		calls = append(calls, progressCall{received, total})
	})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	last := calls[len(calls)-1]
	want := int64(2 * len(chunk))
	if last.received != want || last.total != want {
		t.Fatalf("unknown length: final emit must substitute received for total, got %+v want %d", last, want)
	}
}

func TestDownloadStallAborts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000000")
		w.Write(make([]byte, 100))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := &Client{StallTimeout: 50 * time.Millisecond}
	dst := filepath.Join(t.TempDir(), "asset")
	start := time.Now()
	err := c.download(context.Background(), srv.URL, dst, nil)
	if err == nil {
		t.Fatal("expected stall error, got nil")
	}
	if !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("error should name the stall, got: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("stall abort took %s; watchdog is not firing", elapsed)
	}
}

// The archive can legitimately take minutes on a slow link, so the download
// client must have no whole-request Timeout; the stall watchdog governs
// instead. Lookup keeps its short cap separately.
func TestDownloadClientHasNoTotalTimeout(t *testing.T) {
	c := &Client{}
	if got := c.downloadClient().Timeout; got != 0 {
		t.Fatalf("download client Timeout = %s, want 0 (stall watchdog governs)", got)
	}
	if got := c.httpClient().Timeout; got != lookupTimeout {
		t.Fatalf("lookup client Timeout = %s, want %s", got, lookupTimeout)
	}
}

func TestApplyReportsProgressForArchiveOnly(t *testing.T) {
	buildDir := t.TempDir()
	archivePath, sha256sum, err := buildTarGz(buildDir, "fake-binary-content")
	if err != nil {
		t.Fatalf("build archive: %v", err)
	}
	assetName := filepath.Base(archivePath)
	checksumPath := buildChecksums(buildDir, assetName, sha256sum)

	mux := http.NewServeMux()
	mux.HandleFunc("/"+assetName, func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, archivePath)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, checksumPath)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	binDir := t.TempDir()
	currentBin := filepath.Join(binDir, "atomic")
	if err := os.WriteFile(currentBin, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("seed binary: %v", err)
	}

	rel := Release{
		TagName: "v0.1.1",
		Assets: []Asset{
			{Name: assetName, BrowserDownloadURL: srv.URL + "/" + assetName},
			{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/checksums.txt"},
		},
	}

	info, err := os.Stat(archivePath)
	if err != nil {
		t.Fatalf("stat archive: %v", err)
	}

	var calls []progressCall
	c := &Client{
		DownloadURL: srv.URL,
		OnProgress: func(received, total int64) {
			calls = append(calls, progressCall{received, total})
		},
	}
	if err := c.Apply(context.Background(), rel, currentBin); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(calls) == 0 {
		t.Fatal("Apply emitted no progress for the archive download")
	}
	for i, call := range calls {
		if call.total != info.Size() {
			t.Fatalf("call %d total = %d, want archive size %d — checksums must not report progress", i, call.total, info.Size())
		}
	}
}
