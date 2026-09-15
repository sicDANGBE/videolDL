package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfig_Load_applies_file_environment_and_flag_precedence(t *testing.T) {
	// Given
	home := t.TempDir()
	work := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.json")
	configJSON := `{"destination":"file-downloads","state_path":"file-queue.json","log_path":"file-logs","concurrency":2,"editor":"file-editor","auto_start_worker":true,"daemon_pid_path":"file-daemon.pid","daemon_log_path":"file-daemon.log","notify_command":"file-notify"}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	flags := Overrides{Destination: stringPtr("flag-downloads"), Concurrency: intPtr(4)}
	env := []string{
		"VIDEODL_CONFIG=" + configPath,
		"VIDEODL_DESTINATION=env-downloads",
		"VIDEODL_STATE_PATH=env-queue.json",
		"VIDEODL_CONCURRENCY=3",
		"VIDEODL_EDITOR=env-editor",
		"VIDEODL_AUTO_START_WORKER=false",
		"VIDEODL_DAEMON_PID_PATH=env-daemon.pid",
		"VIDEODL_DAEMON_LOG_PATH=env-daemon.log",
		"VIDEODL_NOTIFY_COMMAND=env-notify",
	}

	// When
	got, err := Load(LoadOptions{HomeDir: home, WorkingDir: work, Env: env, Overrides: flags})

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if got.Destination != filepath.Join(work, "flag-downloads") {
		t.Fatalf("destination = %q", got.Destination)
	}
	if got.StatePath != filepath.Join(work, "env-queue.json") {
		t.Fatalf("state path = %q", got.StatePath)
	}
	if got.LogPath != filepath.Join(work, "file-logs") {
		t.Fatalf("log path = %q", got.LogPath)
	}
	if got.Concurrency != 4 {
		t.Fatalf("concurrency = %d, want 4", got.Concurrency)
	}
	if got.Editor != "env-editor" {
		t.Fatalf("editor = %q", got.Editor)
	}
	if got.AutoStartWorker {
		t.Fatal("auto_start_worker = true, want env override false")
	}
	if got.DaemonPIDPath != filepath.Join(work, "env-daemon.pid") {
		t.Fatalf("daemon pid path = %q", got.DaemonPIDPath)
	}
	if got.DaemonLogPath != filepath.Join(work, "env-daemon.log") {
		t.Fatalf("daemon log path = %q", got.DaemonLogPath)
	}
	if got.NotifyCommand != "env-notify" {
		t.Fatalf("notify_command = %q", got.NotifyCommand)
	}
}

func TestConfig_Load_decodes_new_file_fields(t *testing.T) {
	// Given
	home := t.TempDir()
	work := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.json")
	configJSON := `{"editor":"nvim","auto_start_worker":true,"daemon_pid_path":"daemon/pid","daemon_log_path":"daemon/log","notify_command":"notify-send videodl"}`
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	// When
	got, err := Load(LoadOptions{HomeDir: home, WorkingDir: work, ConfigPath: configPath, Env: []string{}})

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if got.Editor != "nvim" {
		t.Fatalf("editor = %q", got.Editor)
	}
	if !got.AutoStartWorker {
		t.Fatal("auto_start_worker = false, want true")
	}
	if got.DaemonPIDPath != filepath.Join(work, "daemon", "pid") {
		t.Fatalf("daemon pid path = %q", got.DaemonPIDPath)
	}
	if got.DaemonLogPath != filepath.Join(work, "daemon", "log") {
		t.Fatalf("daemon log path = %q", got.DaemonLogPath)
	}
	if got.NotifyCommand != "notify-send videodl" {
		t.Fatalf("notify_command = %q", got.NotifyCommand)
	}
}

func TestConfig_Load_rejects_invalidEnvironmentAutoStartWorker(t *testing.T) {
	// Given
	secret := "super-secret-token"
	env := []string{"VIDEODL_AUTO_START_WORKER=" + secret}

	// When
	_, err := Load(LoadOptions{HomeDir: t.TempDir(), WorkingDir: t.TempDir(), Env: env})

	// Then
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Field != "auto_start_worker" {
		t.Fatalf("error = %v, want typed auto_start_worker validation", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("error exposed malformed environment value")
	}
}
