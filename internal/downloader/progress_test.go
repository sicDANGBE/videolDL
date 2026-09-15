package downloader

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadWithProgress_reports_known_direct_total(t *testing.T) {
	const body = "known-video"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "video/mp4")
		writer.Header().Set("Content-Length", "11")
		_, _ = writer.Write([]byte(body))
	}))
	defer server.Close()

	output := filepath.Join(t.TempDir(), "video.mp4")
	request, err := NewRequest(server.URL+"/video.mp4", output, false)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	var events []Progress
	err = New(server.Client(), "").DownloadWithProgress(context.Background(), request, func(progress Progress) error {
		events = append(events, progress)
		return nil
	})
	if err != nil {
		t.Fatalf("DownloadWithProgress returned error: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected progress events")
	}
	for _, progress := range events {
		if !progress.TotalBytesKnown || progress.TotalBytes != int64(len(body)) {
			t.Fatalf("progress total = %+v", progress)
		}
	}
	if got := events[len(events)-1].BytesDownloaded; got != int64(len(body)) {
		t.Fatalf("final bytes = %d", got)
	}
}

func TestDownloadWithProgress_does_not_fabricate_unknown_direct_total(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "video/mp4")
		writer.WriteHeader(http.StatusOK)
		writer.(http.Flusher).Flush()
		_, _ = writer.Write([]byte("unknown-video"))
	}))
	defer server.Close()

	output := filepath.Join(t.TempDir(), "video.mp4")
	request, err := NewRequest(server.URL+"/video.mp4", output, false)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	err = New(server.Client(), "").DownloadWithProgress(context.Background(), request, func(progress Progress) error {
		if progress.TotalBytesKnown || progress.TotalBytes != 0 {
			t.Fatalf("unknown total was fabricated: %+v", progress)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("DownloadWithProgress returned error: %v", err)
	}
}

func TestDownloadWithProgress_reports_HLS_segment_index_and_total(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/index.m3u8":
			_, _ = writer.Write([]byte("#EXTM3U\n#EXTINF:1,\none.ts\n#EXTINF:1,\ntwo.ts\n"))
		case "/one.ts":
			_, _ = writer.Write([]byte("one"))
		case "/two.ts":
			_, _ = writer.Write([]byte("two"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	output := filepath.Join(t.TempDir(), "stream.ts")
	request, err := NewRequest(server.URL+"/index.m3u8", output, false)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	var indexes []int
	err = New(server.Client(), "").DownloadWithProgress(context.Background(), request, func(progress Progress) error {
		if progress.SegmentTotal != 2 || progress.SegmentIndex < 1 || progress.SegmentIndex > 2 {
			t.Fatalf("segment progress = %+v", progress)
		}
		indexes = append(indexes, progress.SegmentIndex)
		return nil
	})
	if err != nil {
		t.Fatalf("DownloadWithProgress returned error: %v", err)
	}
	if len(indexes) < 2 || indexes[0] != 1 || indexes[len(indexes)-1] != 2 {
		t.Fatalf("segment indexes = %v", indexes)
	}
}

func TestDownloadWithProgress_propagates_callback_error_and_removes_partial_output(t *testing.T) {
	callbackErr := context.Canceled
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = writer.Write([]byte("partial-video"))
	}))
	defer server.Close()

	directory := t.TempDir()
	output := filepath.Join(directory, "video.mp4")
	request, err := NewRequest(server.URL+"/video.mp4", output, false)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	err = New(server.Client(), "").DownloadWithProgress(context.Background(), request, func(Progress) error {
		return callbackErr
	})
	if !errors.Is(err, callbackErr) {
		t.Fatalf("error = %v, want callback error", err)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output state = %v, want no output", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir returned error: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != ".videodl" {
			t.Fatalf("partial files remain: %v", entries)
		}
	}
	state, err := os.ReadDir(filepath.Join(directory, ".videodl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range state {
		if filepath.Ext(entry.Name()) != ".lock" {
			t.Fatalf("partial data remains: %s", entry.Name())
		}
	}
}

func TestDownloadWithProgress_preserves_stale_output_when_callback_cancels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = writer.Write([]byte("new-video"))
	}))
	defer server.Close()

	output := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(output, []byte("old-video"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	request, err := NewRequest(server.URL+"/video.mp4", output, false)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	err = New(server.Client(), "").DownloadWithProgress(context.Background(), request, func(Progress) error {
		return context.Canceled
	})
	if !errors.Is(err, ErrOutputExists) {
		t.Fatalf("error = %v, want existing-output refusal", err)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != "old-video" {
		t.Fatalf("stale output = %q", content)
	}
}
