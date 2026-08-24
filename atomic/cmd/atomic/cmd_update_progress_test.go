package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestDownloadProgressRendererNonTTYIsNil(t *testing.T) {
	if r := downloadProgressRenderer(io.Discard, false); r != nil {
		t.Fatal("non-TTY must yield a nil renderer: without \\r rewriting each tick prints its own line")
	}
}

func TestDownloadProgressRendererRewritesOneLine(t *testing.T) {
	var buf bytes.Buffer
	r := downloadProgressRenderer(&buf, true)

	r(512*1024, 2*1024*1024)
	out := buf.String()
	if !strings.HasPrefix(out, "\r") {
		t.Fatalf("progress must rewrite in place with \\r, got %q", out)
	}
	if !strings.Contains(out, "0.5") || !strings.Contains(out, "2.0") || !strings.Contains(out, "25%") {
		t.Fatalf("progress line missing received/total/percent, got %q", out)
	}
	if strings.HasSuffix(out, "\n") {
		t.Fatalf("mid-stream tick must not end the line, got %q", out)
	}

	buf.Reset()
	r(2*1024*1024, 2*1024*1024)
	out = buf.String()
	if !strings.Contains(out, "100%") || !strings.HasSuffix(out, "\n") {
		t.Fatalf("final tick must print 100%% and end the line, got %q", out)
	}

	buf.Reset()
	r(2*1024*1024, 2*1024*1024)
	if buf.Len() != 0 {
		t.Fatalf("renderer must go quiet after the 100%% line, got %q", buf.String())
	}
}

func TestDownloadProgressRendererUnknownTotal(t *testing.T) {
	var buf bytes.Buffer
	r := downloadProgressRenderer(&buf, true)
	r(300*1024, 0)
	out := buf.String()
	if !strings.Contains(out, "0.3") || strings.Contains(out, "%") {
		t.Fatalf("unknown total renders bare MB with no percent, got %q", out)
	}
}
