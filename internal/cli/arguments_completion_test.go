package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"video-downloader/internal/queue"
)

func TestAddNamesAndShortOptions(t *testing.T) {
	url := "https://example.test/toto.mp4"
	for _, test := range []struct {
		name   string
		args   []string
		output string
	}{
		{"positional", []string{"toto.mp4", url}, "toto.mp4"},
		{"long", []string{"--name", "toto.mp4", url}, "toto.mp4"},
		{"short", []string{"-n", "toto.mp4", url}, "toto.mp4"},
		{"short equals", []string{"-n=toto.mp4", url}, "toto.mp4"},
		{"output", []string{"-o", "toto.mp4", url}, "toto.mp4"},
		{"URL only", []string{url}, "toto.mp4"},
		{"spaces", []string{"mon film.mp4", url}, "mon film.mp4"},
		{"end of options", []string{"--", "-film.mp4", url}, "-film.mp4"},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			state := filepath.Join(home, "queue.json")
			destination := filepath.Join(home, "media")
			config := filepath.Join(home, "profile.json")
			if err := os.WriteFile(config, []byte(`{"retries":8,"resume":true,"ffmpeg":true,"auto_start_worker":false}`), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"add", "-c", config, "-s", state, "-d", destination, "-r", "0", "-R=false", "-f=false", "-H", "720", "-T", "12s", "-J"}
			args = append(args, test.args...)
			var out bytes.Buffer
			if err := Run(context.Background(), Options{Args: args, HomeDir: home, WorkingDir: home, Env: []string{}, Out: &out}); err != nil {
				t.Fatal(err)
			}
			var job queue.Job
			if err := json.Unmarshal(out.Bytes(), &job); err != nil {
				t.Fatal(err)
			}
			if job.Name != test.output || job.URL != url || job.OutputPath != filepath.Join(destination, test.output) {
				t.Fatalf("unexpected added job: name=%s path=%s", job.Name, job.OutputPath)
			}
			o := job.DownloadOptions
			if o.Retries == nil || *o.Retries != 0 || o.Resume == nil || *o.Resume || o.FFmpeg == nil || *o.FFmpeg || o.MaxHeight == nil || *o.MaxHeight != 720 || o.IdleTimeout == nil || *o.IdleTimeout != 12*time.Second {
				t.Fatal("short transfer options did not override the configuration")
			}
			jobs, err := queue.Snapshot(state)
			if err != nil || len(jobs) != 1 || jobs[0].Status != queue.StatusQueued {
				t.Fatal("add did not persist a queued job", err)
			}
		})
	}
}
func TestAddRejectsAmbiguousNamesAndInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{}, {"toto.mp4"}, {"one.mp4", "two.mp4", "https://example.test/v.mp4"},
		{"-n", "one.mp4", "two.mp4", "https://example.test/v.mp4"},
		{"-n", "one.mp4", "-o", "two.mp4", "https://example.test/v.mp4"},
		{"-n=", "https://example.test/v.mp4"}, {"", "https://example.test/v.mp4"}, {"../escape.mp4", "https://example.test/v.mp4"},
		{"toto.mp4", "https:://example.test/playlist.m3u8"}, {"-n"},
	} {
		home := t.TempDir()
		state := filepath.Join(home, "queue.json")
		full := append([]string{"add", "-s", state}, args...)
		if err := Run(context.Background(), Options{Args: full, HomeDir: home, WorkingDir: home, Env: []string{}, Out: &bytes.Buffer{}}); err == nil {
			t.Fatalf("accepted %q", args)
		}
		jobs, err := queue.Snapshot(state)
		if err != nil || len(jobs) != 0 {
			t.Fatalf("invalid add modified queue: %v", err)
		}
	}
}
func TestCompletionUnderstandsCommandAndExpectedValue(t *testing.T) {
	for _, test := range []struct {
		words        []string
		want, forbid []string
		empty        bool
	}{
		{words: []string{"add", "--"}, want: []string{"--name", "--retries", "--destination"}, forbid: []string{"--watch", "--once", "--interval", "--concurrency", "--timeout", "--ffmpeg-path"}},
		{words: []string{"add", "--name", "--"}, empty: true},
		{words: []string{"add", "-n", ""}, empty: true},
		{words: []string{"add", "--name=--"}, empty: true},
		{words: []string{"add", "toto.mp4", ""}, empty: true},
		{words: []string{"add", "toto.mp4", "--"}, empty: true},
		{words: []string{"add", "--name", "film.mp4", "--"}, want: []string{"--config"}, forbid: []string{"--name", "--output"}},
		{words: []string{"worker", "-"}, want: []string{"-j", "-w", "-c"}, forbid: []string{"-n", "-o", "-J", "-d"}},
		{words: []string{"worker", "-j", ""}, want: []string{"1", "8"}, forbid: []string{"--help", "0", "9"}},
		{words: []string{"worker", "--concurrency=", ""}, want: []string{"--config"}, forbid: []string{"--concurrency", "-j"}},
		{words: []string{"worker", "--concurrency=8", "--"}, forbid: []string{"--concurrency"}, want: []string{"--watch"}},
		{words: []string{"worker", "-j=8", "-"}, forbid: []string{"-j", "--concurrency"}},
		{words: []string{"list", "--"}, want: []string{"--config", "--state", "--json", "--help"}, forbid: []string{"--destination", "--concurrency", "--watch", "--timeout"}},
		{words: []string{"watch", "--"}, want: []string{"--once", "--interval"}, forbid: []string{"--name", "--ffmpeg"}},
		{words: []string{"setup", "--"}, want: []string{"--config", "--destination", "--help"}, forbid: []string{"--name", "--concurrency"}},
		{words: []string{"doctor", "--"}, want: []string{"--config", "--help"}, forbid: []string{"--destination", "--json"}},
		{words: []string{"daemon", "status", "--"}, want: []string{"--config", "--help"}, forbid: []string{"--json", "--state"}},
		{words: []string{"config", "edit", "-"}, want: []string{"--editor", "-e", "-c"}, forbid: []string{"--json", "-J"}},
		{words: []string{"config", "show", "-"}, want: []string{"--json", "-J"}, forbid: []string{"--editor"}},
		{words: []string{"config", "get", "destination", ""}, empty: true},
		{words: []string{"config", "set", "resume", ""}, want: []string{"true", "false"}, forbid: []string{"--help", "destination"}},
		{words: []string{"add", "-R="}, want: []string{"-R=true", "-R=false"}},
		{words: []string{"add", "-R", "="}, want: []string{"true", "false"}},
		{words: []string{"add", "-R", "=", "f"}, want: []string{"false"}},
		{words: []string{"add", "--", "--"}, empty: true},
		{words: []string{"add", "--bad", ""}, empty: true},
	} {
		t.Run(strings.Join(test.words, " "), func(t *testing.T) {
			home := t.TempDir()
			got := completionSuggestions(context.Background(), Options{HomeDir: home, WorkingDir: home, Env: []string{}}, test.words)
			if test.empty && len(got) != 0 {
				t.Fatalf("expected no suggestions, got %v", got)
			}
			for _, want := range test.want {
				if !slices.Contains(got, want) {
					t.Fatalf("missing %q in %v", want, got)
				}
			}
			for _, forbid := range test.forbid {
				if slices.Contains(got, forbid) {
					t.Fatalf("invalid %q in %v", forbid, got)
				}
			}
			entries, _ := os.ReadDir(home)
			if len(entries) != 0 {
				t.Fatal("completion wrote files")
			}
		})
	}
}
func TestCompletionPathsAndShortQueueSelector(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "my videos"), 0700); err != nil {
		t.Fatal(err)
	}
	opts := Options{HomeDir: home, WorkingDir: home, Env: []string{}}
	for _, words := range [][]string{{"add", "-d", "my"}, {"add", "--destination", "=", "my"}} {
		got := completionSuggestions(context.Background(), opts, words)
		if !slices.Equal(got, []string{"my videos/"}) {
			t.Fatalf("path suggestions: %v", got)
		}
	}
	if got := completionSuggestions(context.Background(), opts, []string{"add", "-d=my"}); !slices.Equal(got, []string{"-d=my videos/"}) {
		t.Fatal(got)
	}
	state := filepath.Join(home, "my queue.json")
	opts.Args = []string{"add", "-s", state, "toto.mp4", "https://example.test/video"}
	opts.Out = &bytes.Buffer{}
	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	jobs, _ := queue.Snapshot(state)
	for _, args := range [][]string{{"status", "-s", state, ""}, {"status", "--state=" + state, ""}, {"status", "--state", "=", state, ""}} {
		got := completionSuggestions(context.Background(), opts, args)
		if !slices.Contains(got, jobs[0].ID) {
			t.Fatalf("queue selector ignored: %v", args)
		}
	}
}
func TestScopedOptionsAreActuallyRejectedAndHelpListsShortNames(t *testing.T) {
	for _, args := range [][]string{{"add", "--watch"}, {"add", "--concurrency", "2"}, {"list", "--name", "unused"}, {"worker", "--json"}, {"worker", "--destination", "unused"}, {"watch", "--ffmpeg"}, {"config", "get", "--json", "destination"}, {"daemon", "status", "--state", "unused"}} {
		home := t.TempDir()
		if err := Run(context.Background(), Options{Args: args, HomeDir: home, WorkingDir: home, Env: []string{}, Out: &bytes.Buffer{}}); err == nil {
			t.Fatalf("accepted ineffective option: %v", args)
		}
	}
	for _, command := range []string{"add", "worker", "watch", "setup", "doctor"} {
		var out bytes.Buffer
		home := t.TempDir()
		if err := Run(context.Background(), Options{Args: []string{command, "-h"}, HomeDir: home, WorkingDir: home, Env: []string{}, Out: &out}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "-c, --config") || !strings.Contains(out.String(), "-h, --help") {
			t.Fatal(out.String())
		}
		if command == "add" && (!strings.Contains(out.String(), "[NOM] URL") || !strings.Contains(out.String(), "-n, --name")) {
			t.Fatal(out.String())
		}
	}
}
func TestShortOptionsAcrossSetupConfigDoctorAndWatch(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "profile.json")
	var out bytes.Buffer
	for _, args := range [][]string{
		{"setup", "-c", path, "-d", filepath.Join(home, "media")},
		{"config", "set", "-c", path, "concurrency", "2"},
		{"config", "get", "-c", path, "concurrency"},
		{"doctor", "-c", path},
		{"watch", "-c", path, "-J", "-1", "-i", "1s"},
		{"daemon", "status", "-c", path},
	} {
		out.Reset()
		if err := Run(context.Background(), Options{Args: args, HomeDir: home, WorkingDir: home, Env: []string{}, Out: &out}); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if args[0] == "watch" && !json.Valid(out.Bytes()) {
			t.Fatal("watch -J ignored")
		}
		if args[0] == "config" && args[1] == "get" && strings.TrimSpace(out.String()) != "2" {
			t.Fatal(out.String())
		}
	}
}
