package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPResumeAcrossInvocationsAndChangedETag(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			var broken atomic.Bool
			var ranged atomic.Bool
			var changedVersion atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				etag := `"v1"`
				body := "abcdefghij"
				if changedVersion.Load() {
					etag = `"v2"`
					body = "0123456789"
				}
				w.Header().Set("ETag", etag)
				if r.Header.Get("Range") != "" {
					ranged.Store(true)
					if r.Header.Get("Range") != "bytes=4-" || r.Header.Get("If-Range") != `"v1"` {
						t.Errorf("invalid resume headers")
					}
					if r.Header.Get("If-Range") == etag {
						w.Header().Set("Content-Range", "bytes 4-9/10")
						w.Header().Set("Content-Length", "6")
						w.WriteHeader(206)
						io.WriteString(w, body[4:])
						return
					}
				}
				w.Header().Set("Content-Length", "10")
				if !broken.Swap(true) {
					io.WriteString(w, body[:4])
					return
				}
				io.WriteString(w, body)
			}))
			defer server.Close()
			output := filepath.Join(t.TempDir(), "video.bin")
			request, _ := NewRequest(server.URL, output, false)
			dl := New(server.Client(), "")
			settings := DefaultSettings()
			settings.Retries = 0
			dl.Configure(settings)
			if err := dl.Download(context.Background(), request); !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("first error: %v", err)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("partial published")
			}
			changedVersion.Store(changed)
			if err := dl.Download(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(output)
			want := "abcdefghij"
			if changed {
				want = "0123456789"
			}
			if string(data) != want || !ranged.Load() {
				t.Fatalf("resume result %q, range=%v", data, ranged.Load())
			}
		})
	}
}

func TestRetriesTransientStatusButNotPermanent(t *testing.T) {
	for _, status := range []int{503, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					w.WriteHeader(status)
					return
				}
				io.WriteString(w, "ok")
			}))
			defer server.Close()
			request, _ := NewRequest(server.URL, filepath.Join(t.TempDir(), "video.bin"), false)
			err := New(server.Client(), "").Download(context.Background(), request)
			if status == 503 && (err != nil || calls.Load() != 2) {
				t.Fatalf("retry: %v, calls=%d", err, calls.Load())
			}
			if status == 404 && (err == nil || calls.Load() != 1) {
				t.Fatalf("permanent error: %v, calls=%d", err, calls.Load())
			}
		})
	}
}
func TestExistingFileAndConcurrentDownloadAreProtected(t *testing.T) {
	directory := t.TempDir()
	output := filepath.Join(directory, "video.bin")
	first, err := lockDestination(output)
	if err != nil {
		t.Fatal(err)
	}
	defer first.close()
	if second, err := lockDestination(output); !errors.Is(err, ErrDownloadBusy) {
		if second != nil {
			second.close()
		}
		t.Fatalf("lock: %v", err)
	}
	temp := filepath.Join(directory, "ready")
	os.WriteFile(temp, []byte("new"), 0600)
	os.WriteFile(output, []byte("old"), 0600)
	if err := publishOutput(temp, output); !errors.Is(err, ErrOutputExists) {
		t.Fatalf("publish: %v", err)
	}
	data, _ := os.ReadFile(output)
	if string(data) != "old" {
		t.Fatal("existing file changed")
	}
}
func TestIdleTimeoutStopsStalledDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	dl := New(server.Client(), "")
	settings := DefaultSettings()
	settings.Retries = 0
	settings.IdleTimeout = 20 * time.Millisecond
	dl.Configure(settings)
	request, _ := NewRequest(server.URL, filepath.Join(t.TempDir(), "video.bin"), false)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := dl.Download(ctx, request)
	var idle idleError
	if !errors.As(err, &idle) {
		t.Fatalf("idle error: %v", err)
	}
}
func TestHLSHeightSelectionAndUnknownResolution(t *testing.T) {
	parsed, err := parsePlaylist(strings.NewReader("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=100,RESOLUTION=640x360\nlow.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=900,RESOLUTION=1920x1080\nhigh.m3u8\n"))
	if err != nil {
		t.Fatal(err)
	}
	selected, err := selectVariant(parsed.variants, 720)
	if err != nil || selected.URI != "low.m3u8" {
		t.Fatalf("selected: %+v, %v", selected, err)
	}
	if _, err := selectVariant([]variant{{URI: "unknown", Bandwidth: 999}}, 720); err == nil {
		t.Fatal("unknown resolution selected")
	}
}
func TestRetryWaitIsCancelable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(retryPause(ctx, 3, "300"), context.Canceled) {
		t.Fatal("retry wait ignores cancellation")
	}
}

func TestInvalidContentRangeNeverPublishesMixedData(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Length", "10")
			io.WriteString(w, "abcd")
			return
		}
		w.Header().Set("Content-Range", "bytes 5-9/10")
		w.WriteHeader(206)
		io.WriteString(w, "fghij")
	}))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "video.bin")
	request, _ := NewRequest(server.URL, output, false)
	dl := New(server.Client(), "")
	settings := DefaultSettings()
	settings.Retries = 0
	dl.Configure(settings)
	if err := dl.Download(context.Background(), request); err == nil {
		t.Fatal("expected broken transfer")
	}
	if err := dl.Download(context.Background(), request); err == nil {
		t.Fatal("accepted inconsistent content range")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("invalid file published")
	}
}
func TestSlowProgressDoesNotTriggerNetworkIdleTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "video") }))
	defer server.Close()
	dl := New(server.Client(), "")
	settings := DefaultSettings()
	settings.Retries = 0
	settings.IdleTimeout = 10 * time.Millisecond
	dl.Configure(settings)
	request, _ := NewRequest(server.URL, filepath.Join(t.TempDir(), "video.bin"), false)
	if err := dl.DownloadWithProgress(context.Background(), request, func(Progress) error { time.Sleep(30 * time.Millisecond); return nil }); err != nil {
		t.Fatal(err)
	}
}
