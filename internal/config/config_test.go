package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfig_Load_uses_defaults(t *testing.T) {
	// Given
	home := t.TempDir()
	work := filepath.Join(t.TempDir(), "work")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatal(err)
	}

	// When
	got, err := Load(LoadOptions{HomeDir: home, WorkingDir: work, Env: []string{}})

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if got.Concurrency != 1 {
		t.Fatalf("concurrency = %d, want 1", got.Concurrency)
	}
	wantDestination := filepath.Join(home, "Downloads", "videodl")
	if got.Destination != wantDestination {
		t.Fatalf("destination = %q, want %q", got.Destination, wantDestination)
	}
	if !filepath.IsAbs(got.Destination) {
		t.Fatalf("destination %q is not absolute", got.Destination)
	}
	if got.ConfigPath != filepath.Join(home, ".config", "videodl", "config.json") {
		t.Fatalf("config path = %q", got.ConfigPath)
	}
	if got.StatePath != filepath.Join(home, ".local", "state", "videodl", "queue.json") {
		t.Fatalf("state path = %q", got.StatePath)
	}
	if got.LogPath != filepath.Join(home, ".local", "state", "videodl", "logs") {
		t.Fatalf("log path = %q", got.LogPath)
	}
	if got.DaemonPIDPath != filepath.Join(home, ".local", "state", "videodl", "daemon.pid") {
		t.Fatalf("daemon pid path = %q", got.DaemonPIDPath)
	}
	if got.DaemonLogPath != filepath.Join(home, ".local", "state", "videodl", "logs", "daemon.log") {
		t.Fatalf("daemon log path = %q", got.DaemonLogPath)
	}
	if got.Editor != "" {
		t.Fatalf("editor = %q, want empty", got.Editor)
	}
	if got.AutoStartWorker {
		t.Fatal("auto_start_worker = true, want false")
	}
	if got.NotifyCommand != "" {
		t.Fatalf("notify_command = %q, want empty", got.NotifyCommand)
	}
}

func TestConfig_Load_uses_xdg_defaults_without_existing_config(t *testing.T) {
	// Given
	home := t.TempDir()
	work := t.TempDir()
	configHome := filepath.Join(t.TempDir(), "xdg-config")
	stateHome := filepath.Join(t.TempDir(), "xdg-state")
	env := []string{"XDG_CONFIG_HOME=" + configHome, "XDG_STATE_HOME=" + stateHome}

	// When
	got, err := Load(LoadOptions{HomeDir: home, WorkingDir: work, Env: env})

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if got.ConfigPath != filepath.Join(configHome, "videodl", "config.json") {
		t.Fatalf("config path = %q", got.ConfigPath)
	}
	if got.StatePath != filepath.Join(stateHome, "videodl", "queue.json") {
		t.Fatalf("state path = %q", got.StatePath)
	}
	if got.LogPath != filepath.Join(stateHome, "videodl", "logs") {
		t.Fatalf("log path = %q", got.LogPath)
	}
	if got.Destination != filepath.Join(home, "Downloads", "videodl") {
		t.Fatalf("destination = %q", got.Destination)
	}
}

func stringPtr(value string) *string { return &value }

func intPtr(value int) *int { return &value }
