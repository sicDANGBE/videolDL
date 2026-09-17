package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_completion_scripts_include_commands_and_flags(t *testing.T) {
	commands := []string{"add", "worker", "list", "status", "retry", "cancel", "config", "daemon", "watch", "completion"}
	flags := []string{"--config", "--state", "--log", "--destination", "--concurrency", "--timeout", "--ffmpeg", "--ffmpeg-path", "--webhook", "--json", "--help"}

	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			// Given
			var output bytes.Buffer

			// When
			err := Run(context.Background(), Options{Args: []string{"completion", shell}, Out: &output, ErrOut: ioDiscard{}})

			// Then
			if err != nil {
				t.Fatalf("completion %s returned error: %v", shell, err)
			}
			content := output.String()
			if !strings.Contains(content, "__complete "+shell) {
				t.Fatalf("script does not call hidden completion: %q", content)
			}
			for _, command := range commands {
				if !strings.Contains(content, command) {
					t.Fatalf("script for %s missing command %q: %q", shell, command, content)
				}
			}
			for _, flag := range flags {
				if !strings.Contains(content, flag) {
					t.Fatalf("script for %s missing flag %q: %q", shell, flag, content)
				}
			}
		})
	}
}

func TestRun_completion_rejects_unsupported_shell(t *testing.T) {
	// Given
	var output bytes.Buffer

	// When
	err := Run(context.Background(), Options{Args: []string{"completion", "powershell"}, Out: &output, ErrOut: ioDiscard{}})

	// Then
	if err == nil {
		t.Fatal("completion powershell unexpectedly succeeded")
	}
}

func TestRun_hidden_completion_returns_static_suggestions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "top level commands", args: []string{"__complete", "bash", ""}, want: []string{"add", "worker", "list", "status", "retry", "cancel", "config", "daemon", "watch", "completion"}},
		{name: "list flags", args: []string{"__complete", "zsh", "list", "--"}, want: []string{"--config", "--state", "--json", "--help"}},
		{name: "completion shells", args: []string{"__complete", "fish", "completion", ""}, want: []string{"bash", "zsh", "fish"}},
		{name: "config keys", args: []string{"__complete", "bash", "config", "get", ""}, want: []string{"state_path", "log_path", "destination", "concurrency", "timeout", "ffmpeg", "ffmpeg_path", "webhook_url", "editor", "auto_start_worker", "daemon_pid_path", "daemon_log_path", "notify_command"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			var output bytes.Buffer

			// When
			err := Run(context.Background(), Options{Args: tt.args, Out: &output, ErrOut: ioDiscard{}})

			// Then
			if err != nil {
				t.Fatalf("hidden completion returned error: %v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(output.String(), want+"\n") {
					t.Fatalf("suggestions missing %q: %q", want, output.String())
				}
			}
		})
	}
}

func TestRun_hidden_completion_returns_job_ids_for_job_commands(t *testing.T) {
	// Given
	workDir := t.TempDir()
	state := filepath.Join(workDir, "queue.json")
	queueConfig := completionQueueConfig{workDir: workDir, state: state}
	firstID := addCompletionJob(t, queueConfig, "one.webm")
	secondID := addCompletionJob(t, queueConfig, "two.webm")

	for _, command := range []string{"status", "retry", "cancel"} {
		t.Run(command, func(t *testing.T) {
			var output bytes.Buffer

			// When
			err := Run(context.Background(), Options{Args: []string{"__complete", "bash", command, "--state", state, ""}, Out: &output, ErrOut: ioDiscard{}, WorkingDir: workDir})

			// Then
			if err != nil {
				t.Fatalf("hidden completion for %s returned error: %v", command, err)
			}
			content := output.String()
			if !strings.Contains(content, firstID+"\n") || !strings.Contains(content, secondID+"\n") {
				t.Fatalf("job suggestions for %s = %q, want %q and %q", command, content, firstID, secondID)
			}
		})
	}
}

func TestRun_hidden_completion_degrades_when_queue_unavailable(t *testing.T) {
	// Given
	workDir := t.TempDir()
	var output bytes.Buffer

	// When
	err := Run(context.Background(), Options{Args: []string{"__complete", "bash", "status", "", "--state", filepath.Join(workDir, "missing", "queue.json")}, Out: &output, ErrOut: ioDiscard{}, WorkingDir: workDir})

	// Then
	if err != nil {
		t.Fatalf("hidden completion returned error for unavailable queue: %v", err)
	}
}

func TestRun_hidden_completion_does_not_create_downloads(t *testing.T) {
	// Given
	workDir := t.TempDir()
	var output bytes.Buffer

	// When
	err := Run(context.Background(), Options{Args: []string{"__complete", "bash", "status", ""}, Out: &output, ErrOut: ioDiscard{}, WorkingDir: workDir})

	// Then
	if err != nil {
		t.Fatalf("hidden completion returned error: %v", err)
	}
	if strings.Contains(output.String(), "job-") {
		t.Fatalf("unexpected job suggestions: %q", output.String())
	}
	_, statErr := os.Stat(filepath.Join(workDir, "downloads"))
	if statErr == nil {
		t.Fatal("hidden completion created downloads directory")
	}
	if !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stat downloads: %v", statErr)
	}
}

type completionQueueConfig struct {
	workDir string
	state   string
}

func addCompletionJob(t *testing.T, queueConfig completionQueueConfig, name string) string {
	t.Helper()
	var addOutput bytes.Buffer
	args := []string{"add", "--state", queueConfig.state, "--destination", queueConfig.workDir, "--name", name, "--json", "https://example.test/" + name}
	if err := Run(context.Background(), Options{Args: args, Out: &addOutput, ErrOut: ioDiscard{}, WorkingDir: queueConfig.workDir}); err != nil {
		t.Fatalf("add returned error: %v", err)
	}
	var job struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(addOutput.Bytes(), &job); err != nil {
		t.Fatalf("decode added job: %v", err)
	}
	return job.ID
}
