package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"video-downloader/internal/queue"
)

func TestRun_watchJSON_reports_totals_and_daemon_status_when_jobs_exist(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	statePath := filepath.Join(workDir, "queue.json")
	store := seedWatchJobs(t, statePath)
	_, failedJob := finishWatchJobs(t, store)
	var output bytes.Buffer

	// When
	err := Run(context.Background(), Options{Args: []string{"watch", "--state", statePath, "--json", "--once"}, Out: &output, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}})

	// Then
	if err != nil {
		t.Fatalf("watch returned error: %v", err)
	}
	var report struct {
		Totals struct {
			Queued    int `json:"queued"`
			Running   int `json:"running"`
			Completed int `json:"completed"`
			Failed    int `json:"failed"`
		} `json:"totals"`
		Daemon struct {
			Status string `json:"status"`
		} `json:"daemon"`
		Recent []queue.Job `json:"recent_events"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("watch JSON could not be decoded: %v; output=%q", err, output.String())
	}
	if report.Totals.Queued != 1 || report.Totals.Running != 1 || report.Totals.Completed != 1 || report.Totals.Failed != 1 {
		t.Fatalf("totals = %#v", report.Totals)
	}
	if report.Daemon.Status != "stopped" {
		t.Fatalf("daemon status = %q", report.Daemon.Status)
	}
	if len(report.Recent) == 0 || report.Recent[len(report.Recent)-1].ID != failedJob.ID {
		t.Fatalf("recent events = %#v, want failed job %s", report.Recent, failedJob.ID)
	}
}

func TestRun_watchText_includes_job_ids_and_errors_when_failures_exist(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	statePath := filepath.Join(workDir, "queue.json")
	store := seedWatchJobs(t, statePath)
	completedJob, failedJob := finishWatchJobs(t, store)
	var output bytes.Buffer

	// When
	err := Run(context.Background(), Options{Args: []string{"watch", "--state", statePath, "--once"}, Out: &output, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}})

	// Then
	if err != nil {
		t.Fatalf("watch returned error: %v", err)
	}
	text := output.String()
	for _, want := range []string{"daemon=stopped", "queued=1", "running=1", "completed=1", "failed=1", completedJob.ID, failedJob.ID, "network broke"} {
		if !strings.Contains(text, want) {
			t.Fatalf("watch output missing %q: %s", want, text)
		}
	}
}

func TestRun_worker_keeps_completed_state_when_notify_command_fails(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	statePath := filepath.Join(workDir, "queue.json")
	destination := filepath.Join(workDir, "downloads")
	recordPath := filepath.Join(workDir, "notify.txt")
	notifierPath := writeNotifyScript(t, workDir)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("video"))
	}))
	defer server.Close()
	env := []string{"VIDEODL_NOTIFY_COMMAND=" + notifierPath + " {state} {job_id} {name}", "VIDEODL_NOTIFY_RECORD=" + recordPath, "VIDEODL_NOTIFY_EXIT=1"}
	var output bytes.Buffer
	if err := Run(context.Background(), Options{Args: []string{"add", "--state", statePath, "--destination", destination, "--name", "clip.webm", server.URL + "/clip.webm"}, Out: &output, ErrOut: &output, HomeDir: home, WorkingDir: workDir, Env: env}); err != nil {
		t.Fatalf("add returned error: %v", err)
	}

	// When
	err := Run(context.Background(), Options{Args: []string{"worker", "--state", statePath}, Out: &output, ErrOut: &output, Client: server.Client(), HomeDir: home, WorkingDir: workDir, Env: env})

	// Then
	if err != nil {
		t.Fatalf("worker returned error after notify failure: %v", err)
	}
	store, err := queue.NewStore(statePath)
	if err != nil {
		t.Fatalf("open queue store: %v", err)
	}
	jobs, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Status != queue.StatusCompleted {
		t.Fatalf("jobs after notify failure = %#v", jobs)
	}
	data, err := os.ReadFile(filepath.Join(home, ".local", "state", "videodl", "logs", "notify-errors.jsonl"))
	if err != nil {
		t.Fatalf("read notify error log: %v", err)
	}
	if !strings.Contains(string(data), "exit status 1") {
		t.Fatalf("notify error log = %s", data)
	}
}

func seedWatchJobs(t *testing.T, statePath string) *queue.Store {
	t.Helper()
	store, err := queue.NewStore(statePath)
	if err != nil {
		t.Fatalf("open queue store: %v", err)
	}
	for _, input := range []queue.AddInput{
		{URL: "https://example.test/queued.webm", Name: "queued.webm", OutputPath: "/tmp/queued.webm"},
		{URL: "https://example.test/running.webm", Name: "running.webm", OutputPath: "/tmp/running.webm"},
		{URL: "https://example.test/completed.webm", Name: "completed.webm", OutputPath: "/tmp/completed.webm"},
		{URL: "https://example.test/failed.webm", Name: "failed.webm", OutputPath: "/tmp/failed.webm"},
	} {
		if _, err := store.Add(context.Background(), input); err != nil {
			t.Fatalf("add job: %v", err)
		}
	}
	return store
}

func finishWatchJobs(t *testing.T, store *queue.Store) (queue.Job, queue.Job) {
	t.Helper()
	jobs, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list seeded jobs: %v", err)
	}
	if _, err := store.Claim(context.Background(), jobs[1].ID); err != nil {
		t.Fatalf("claim running job: %v", err)
	}
	completed, err := store.Claim(context.Background(), jobs[2].ID)
	if err != nil {
		t.Fatalf("claim completed job: %v", err)
	}
	if err := store.Complete(context.Background(), completed.ID); err != nil {
		t.Fatalf("complete job: %v", err)
	}
	failed, err := store.Claim(context.Background(), jobs[3].ID)
	if err != nil {
		t.Fatalf("claim failed job: %v", err)
	}
	if err := store.Fail(context.Background(), failed.ID, errors.New("network broke")); err != nil {
		t.Fatalf("fail job: %v", err)
	}
	jobs, err = store.List(context.Background())
	if err != nil {
		t.Fatalf("list finished jobs: %v", err)
	}
	return jobs[2], jobs[3]
}

func writeNotifyScript(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "notify.sh")
	content := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$VIDEODL_NOTIFY_RECORD\"\nexit \"${VIDEODL_NOTIFY_EXIT:-0}\"\n"
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("write notifier: %v", err)
	}
	return path
}
