package downloader

import (
	"errors"
	"testing"
)

func TestNewRequest_rejects_non_http_url(t *testing.T) {
	_, err := NewRequest("file:///tmp/video.mp4", "video.mp4", false)
	if !errors.Is(err, ErrInvalidURL) {
		t.Fatalf("expected ErrInvalidURL, got %v", err)
	}
}

func TestNewRequest_preserves_video_url_and_output(t *testing.T) {
	request, err := NewRequest("https://example.com/video.webm", "downloads/video.webm", false)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	if got := request.Source().String(); got != "https://example.com/video.webm" {
		t.Fatalf("source = %q", got)
	}
	if got := request.OutputPath(); got != "downloads/video.webm" {
		t.Fatalf("output = %q", got)
	}
}
