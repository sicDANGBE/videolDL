package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloader_downloads_direct_video_stream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "video/mp4")
		_, _ = writer.Write([]byte("direct-video"))
	}))
	defer server.Close()

	output := filepath.Join(t.TempDir(), "video.mp4")
	video, err := NewRequest(server.URL+"/video.mp4", output, false)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	if err := New(http.DefaultClient, "").Download(context.Background(), video); err != nil {
		t.Fatalf("Download returned error: %v", err)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != "direct-video" {
		t.Fatalf("content = %q", content)
	}
}

func TestDownloader_downloads_master_playlist_with_relative_segments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/master.m3u8":
			writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = writer.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=400\nlow/index.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=900\nhigh/index.m3u8\n"))
		case "/high/index.m3u8":
			writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = writer.Write([]byte("#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:2,\nsegments/one.m4s\n#EXTINF:2,\nsegments/two.m4s\n"))
		case "/high/init.mp4":
			_, _ = writer.Write([]byte("init|"))
		case "/high/segments/one.m4s":
			_, _ = writer.Write([]byte("one|"))
		case "/high/segments/two.m4s":
			_, _ = writer.Write([]byte("two"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	output := filepath.Join(t.TempDir(), "stream.mp4")
	video, err := NewRequest(server.URL+"/master.m3u8", output, false)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	if err := New(server.Client(), "").Download(context.Background(), video); err != nil {
		t.Fatalf("Download returned error: %v", err)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != "init|one|two" {
		t.Fatalf("content = %q", content)
	}
}
