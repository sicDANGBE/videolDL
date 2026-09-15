package downloader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHLSRemuxProducesMP4WithAudio(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	source := t.TempDir()
	playlist := filepath.Join(source, "index.m3u8")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=10", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "1", "-c:v", "mpeg2video", "-c:a", "aac", "-f", "hls", "-hls_time", "0.5", playlist)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, data)
	}
	server := httptest.NewServer(http.FileServer(http.Dir(source)))
	defer server.Close()
	output := filepath.Join(t.TempDir(), "result.mp4")
	request, _ := NewRequest(server.URL+"/index.m3u8", output, false)
	if err := New(server.Client(), ffmpeg).Download(ctx, request); err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(output)
	if err != nil || len(bytes) < 8 || string(bytes[4:8]) != "ftyp" {
		t.Fatal("output is not an MP4 container")
	}
	data, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-show_entries", "stream=codec_type", "-of", "csv=p=0", output).CombinedOutput()
	if err != nil || !strings.Contains(string(data), "video") || !strings.Contains(string(data), "audio") {
		t.Fatalf("missing streams: %v %s", err, data)
	}
}
