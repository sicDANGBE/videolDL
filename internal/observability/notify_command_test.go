package observability

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogger_Emit_notifyCommand_receives_success_and_failure_context(t *testing.T) {
	// Given
	directory := t.TempDir()
	recordPath := filepath.Join(directory, "notify.txt")
	notifierPath := writeTestNotifier(t, directory)
	t.Setenv("VIDEODL_NOTIFY_RECORD", recordPath)
	logger, err := New(Options{LogDir: directory, NotifyCommand: notifierPath + " {state} {job_id} {name} {error}"})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	// When
	if err := logger.Emit(context.Background(), testEvent(StateSucceeded)); err != nil {
		t.Fatalf("emit success: %v", err)
	}
	if err := logger.Emit(context.Background(), testEvent(StateFailed)); err != nil {
		t.Fatalf("emit failure: %v", err)
	}

	// Then
	data, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read notify record: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("notify records = %q", data)
	}
	if !strings.Contains(lines[0], "succeeded job-7 clip.mp4") {
		t.Fatalf("success notify = %q", lines[0])
	}
	if !strings.Contains(lines[1], "failed job-7 clip.mp4 download failed") {
		t.Fatalf("failure notify = %q", lines[1])
	}
}

func TestLogger_Emit_notifyCommand_exitOne_is_non_fatal_and_logged(t *testing.T) {
	// Given
	directory := t.TempDir()
	notifierPath := writeTestNotifier(t, directory)
	t.Setenv("VIDEODL_NOTIFY_RECORD", filepath.Join(directory, "notify.txt"))
	t.Setenv("VIDEODL_NOTIFY_EXIT", "1")
	logger, err := New(Options{LogDir: directory, NotifyCommand: notifierPath + " {state} {job_id}"})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	// When
	err = logger.Emit(context.Background(), testEvent(StateSucceeded))

	// Then
	if err != nil {
		t.Fatalf("notify failure became event failure: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "notify-errors.jsonl"))
	if err != nil {
		t.Fatalf("read notify error log: %v", err)
	}
	if !strings.Contains(string(data), "exit status 1") {
		t.Fatalf("notify error log = %s", data)
	}
}

func writeTestNotifier(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "notify.sh")
	content := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$VIDEODL_NOTIFY_RECORD\"\nexit \"${VIDEODL_NOTIFY_EXIT:-0}\"\n"
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("write notifier: %v", err)
	}
	return path
}
