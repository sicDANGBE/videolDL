package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_downloads_video_and_reports_completion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("cli-video"))
	}))
	defer server.Close()

	output := filepath.Join(t.TempDir(), "video.webm")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(context.Background(), Options{
		Args:   []string{"-o", output, server.URL + "/video.webm"},
		Out:    &stdout,
		ErrOut: &stderr,
		Client: server.Client(),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != "cli-video" {
		t.Fatalf("content = %q", content)
	}
	if !strings.Contains(stdout.String(), "Téléchargement terminé") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRun_help_exits_successfully(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(context.Background(), Options{
		Args:   []string{"--help"},
		Out:    &stdout,
		ErrOut: &stderr,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !strings.Contains(stdout.String(), "videodl setup") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRun_add_does_not_fetch_until_worker(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		_, _ = writer.Write([]byte("queued-video"))
	}))
	defer server.Close()
	workDir := t.TempDir()
	state := filepath.Join(workDir, "queue.json")
	destination := filepath.Join(workDir, "downloads")
	args := []string{"add", "--state", state, "--destination", destination, "--name", "clip.webm", server.URL + "/clip.webm"}
	var stdout bytes.Buffer
	if err := Run(context.Background(), Options{Args: args, Out: &stdout, ErrOut: &stdout, WorkingDir: workDir}); err != nil {
		t.Fatalf("add returned error: %v", err)
	}
	if requests != 0 {
		t.Fatalf("requests before worker = %d", requests)
	}
	var jobs []map[string]any
	data, err := os.ReadFile(state)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if err := json.Unmarshal(data, &struct {
		Jobs *[]map[string]any `json:"jobs"`
	}{Jobs: &jobs}); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs = %d", len(jobs))
	}

	if err := Run(context.Background(), Options{Args: []string{"worker", "--state", state, "--destination", destination}, Out: &stdout, ErrOut: &stdout, Client: server.Client(), WorkingDir: workDir}); err != nil {
		t.Fatalf("worker returned error: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests after worker = %d", requests)
	}
	content, err := os.ReadFile(filepath.Join(destination, "clip.webm"))
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(content) != "queued-video" {
		t.Fatalf("content = %q", content)
	}
}

func TestRun_list_and_status_support_json(t *testing.T) {
	workDir := t.TempDir()
	state := filepath.Join(workDir, "queue.json")
	args := []string{"add", "--state", state, "--destination", workDir, "--name", "clip.webm", "https://example.test/clip.webm"}
	if err := Run(context.Background(), Options{Args: args, Out: ioDiscard{}, ErrOut: ioDiscard{}, WorkingDir: workDir}); err != nil {
		t.Fatalf("add returned error: %v", err)
	}
	var listed bytes.Buffer
	if err := Run(context.Background(), Options{Args: []string{"list", "--state", state, "--json"}, Out: &listed, ErrOut: &listed, WorkingDir: workDir}); err != nil {
		t.Fatalf("list returned error: %v", err)
	}
	var jobs []map[string]any
	if err := json.Unmarshal(listed.Bytes(), &jobs); err != nil || len(jobs) != 1 {
		t.Fatalf("list JSON = %q, err = %v", listed.String(), err)
	}
	id, ok := jobs[0]["id"].(string)
	if !ok || id == "" {
		t.Fatalf("list id = %#v", jobs[0]["id"])
	}
	var status bytes.Buffer
	if err := Run(context.Background(), Options{Args: []string{"status", "--state", state, "--json", id}, Out: &status, ErrOut: &status, WorkingDir: workDir}); err != nil {
		t.Fatalf("status returned error: %v", err)
	}
	if !strings.Contains(status.String(), id) || !strings.Contains(status.String(), "queued") {
		t.Fatalf("status = %q", status.String())
	}
}

func TestRun_queue_commands_use_global_defaults_without_config(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	var output bytes.Buffer

	// When
	err := Run(context.Background(), Options{
		Args:       []string{"add", "--name", "clip.webm", "https://example.test/clip.webm"},
		Out:        &output,
		ErrOut:     &output,
		HomeDir:    home,
		WorkingDir: workDir,
		Env:        []string{},
	})

	// Then
	if err != nil {
		t.Fatalf("add returned error: %v", err)
	}
	statePath := filepath.Join(home, ".local", "state", "videodl", "queue.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read default state: %v", err)
	}
	if !strings.Contains(string(data), filepath.Join(home, "Downloads", "videodl", "clip.webm")) {
		t.Fatalf("state = %q, want global home destination", data)
	}
	var listed bytes.Buffer
	if err := Run(context.Background(), Options{Args: []string{"list", "--json"}, Out: &listed, ErrOut: &listed, HomeDir: home, WorkingDir: workDir, Env: []string{}}); err != nil {
		t.Fatalf("list returned error: %v", err)
	}
	var jobs []map[string]any
	if err := json.Unmarshal(listed.Bytes(), &jobs); err != nil || len(jobs) != 1 {
		t.Fatalf("list JSON = %q, err = %v", listed.String(), err)
	}
}

func TestRun_retry_and_cancel_update_state(t *testing.T) {
	workDir := t.TempDir()
	state := filepath.Join(workDir, "queue.json")
	var output bytes.Buffer
	if err := Run(context.Background(), Options{Args: []string{"add", "--state", state, "--destination", workDir, "--name", "clip.webm", "https://example.test/clip.webm"}, Out: &output, ErrOut: &output, WorkingDir: workDir}); err != nil {
		t.Fatalf("add returned error: %v", err)
	}
	var jobs []map[string]any
	if err := Run(context.Background(), Options{Args: []string{"list", "--state", state, "--json"}, Out: &output, ErrOut: &output, WorkingDir: workDir}); err != nil {
		t.Fatalf("list returned error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &jobs); err != nil {
		t.Fatalf("decode jobs: %v", err)
	}
	id := jobs[0]["id"].(string)
	if err := Run(context.Background(), Options{Args: []string{"cancel", "--state", state, id}, Out: &output, ErrOut: &output, WorkingDir: workDir}); err != nil {
		t.Fatalf("cancel returned error: %v", err)
	}
	if err := Run(context.Background(), Options{Args: []string{"retry", "--state", state, id}, Out: &output, ErrOut: &output, WorkingDir: workDir}); err != nil {
		t.Fatalf("retry returned error: %v", err)
	}
	if !strings.Contains(output.String(), "queued") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestRun_rejects_invalid_command_url_and_path(t *testing.T) {
	workDir := t.TempDir()
	for _, args := range [][]string{{"unknown"}, {"add", "--state", filepath.Join(workDir, "queue.json"), "--destination", workDir, "not-a-url"}, {"add", "--state", filepath.Join(workDir, "queue.json"), "--destination", workDir, "--name", "../escape", "https://example.test/video"}} {
		if err := Run(context.Background(), Options{Args: args, Out: ioDiscard{}, ErrOut: ioDiscard{}, WorkingDir: workDir}); err == nil {
			t.Fatalf("args %q unexpectedly succeeded", args)
		}
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(data []byte) (int, error) { return len(data), nil }
