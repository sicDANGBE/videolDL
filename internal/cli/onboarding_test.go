package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupCreatesUsablePrivateBundleAndPreservesConfig(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	var out bytes.Buffer
	opts := Options{HomeDir: home, WorkingDir: work, Env: []string{}, Out: &out}
	opts.Args = []string{"setup", "--destination", filepath.Join(work, "media")}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(home, ".config", "videodl")
	path := filepath.Join(directory, "config.json")
	for _, relative := range []string{"config.json", "README.md", "videodl.1", "completions/videodl.bash", "completions/videodl.zsh", "completions/videodl.fish"} {
		if _, err := os.Stat(filepath.Join(directory, relative)); err != nil {
			t.Fatal(err)
		}
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("config permissions: %v", info.Mode())
	}
	before, _ := os.ReadFile(path)
	opts.Args = []string{"setup"}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("setup changed config")
	}
	opts.Args = []string{"doctor"}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	opts.Args = []string{"config", "set", "retries", "0"}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	valid, _ := os.ReadFile(path)
	opts.Args = []string{"config", "set", "retries", "11"}
	if err := Run(context.Background(), opts); err == nil {
		t.Fatal("invalid retries accepted")
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(valid, after) {
		t.Fatal("invalid setting changed config")
	}
}
func TestHelpAndCompletionDoNotCreateState(t *testing.T) {
	for _, args := range [][]string{{}, {"help"}, {"help", "worker"}, {"man"}, {"__complete", "bash", "status", ""}, {"doctor"}} {
		home := t.TempDir()
		var out bytes.Buffer
		if err := Run(context.Background(), Options{Args: args, HomeDir: home, WorkingDir: home, Env: []string{}, Out: &out}); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		files, _ := os.ReadDir(home)
		if len(files) != 0 {
			t.Fatalf("%v wrote files: %v", args, files)
		}
	}
}
func TestConfigShowMasksNotificationSecrets(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	opts := Options{Args: []string{"setup"}, HomeDir: home, WorkingDir: home, Env: []string{}, Out: &out}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	opts.Args = []string{"config", "set", "webhook_url", "https://example.test/private-token"}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	opts.Args = []string{"config", "show", "--json"}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "private-token") {
		t.Fatal("config show exposed notification URL")
	}
}

func TestQueuedTransferOptionsArePersisted(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	opts := Options{HomeDir: home, WorkingDir: home, Env: []string{}, Out: &out}
	opts.Args = []string{"add", "--max-height", "720", "--retries", "0", "--resume=false", "--ffmpeg=false", "--name", "video.mp4", "https://example.test/master.m3u8"}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	state, err := os.ReadFile(filepath.Join(home, ".local", "state", "videodl", "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{`"max_height": 720`, `"retries": 0`, `"resume": false`, `"ffmpeg": false`} {
		if !bytes.Contains(state, []byte(text)) {
			t.Fatalf("missing %s", text)
		}
	}
}

func TestStoredHLSQualityAndWorkerOverride(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(fmt.Sprint(override), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/master.m3u8":
					io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=100,RESOLUTION=640x360\nlow.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=900,RESOLUTION=1920x1080\nhigh.m3u8\n")
				case "/low.m3u8":
					io.WriteString(w, "#EXTM3U\n#EXTINF:1,\nlow.ts\n")
				case "/high.m3u8":
					io.WriteString(w, "#EXTM3U\n#EXTINF:1,\nhigh.ts\n")
				case "/low.ts":
					io.WriteString(w, "low")
				case "/high.ts":
					io.WriteString(w, "high")
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			home := t.TempDir()
			var out bytes.Buffer
			opts := Options{HomeDir: home, WorkingDir: home, Env: []string{}, Out: &out, Client: server.Client()}
			opts.Args = []string{"add", "--max-height", "720", "--name", "result.ts", server.URL + "/master.m3u8"}
			if err := Run(context.Background(), opts); err != nil {
				t.Fatal(err)
			}
			opts.Args = []string{"worker"}
			if override {
				opts.Args = append(opts.Args, "--max-height", "1080")
			}
			if err := Run(context.Background(), opts); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(home, "Downloads", "videodl", "result.ts"))
			if err != nil {
				t.Fatal(err)
			}
			want := "low"
			if override {
				want = "high"
			}
			if string(data) != want {
				t.Fatalf("media=%q, want %q", data, want)
			}
		})
	}
}
