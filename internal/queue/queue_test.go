package queue

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStore_add_list_get_persists_job(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "queue.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	// When
	job, err := store.Add(context.Background(), AddInput{
		URL:        "https://example.test/video",
		Name:       "episode-01",
		OutputPath: filepath.Join(filepath.Dir(path), "episode-01.mp4"),
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Then
	if len(items) != 1 || items[0].ID != job.ID {
		t.Fatalf("list = %#v", items)
	}
	if got.URL != job.URL || got.Name != job.Name || got.OutputPath != job.OutputPath {
		t.Fatalf("job = %#v", got)
	}
	if got.Status != StatusQueued || got.Attempts != 0 {
		t.Fatalf("initial state = %#v", got)
	}
}

func TestStore_retry_and_cancel_update_status_and_attempts(t *testing.T) {
	// Given
	store := newTestStore(t)
	job, err := store.Add(context.Background(), AddInput{URL: "https://example.test", Name: "video", OutputPath: filepath.Join(t.TempDir(), "video")})
	if err != nil {
		t.Fatal(err)
	}
	// When
	if err := store.Retry(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	retried, err := store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Cancel(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	canceled, err := store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Then
	if retried.Status != StatusQueued || retried.Attempts != 1 {
		t.Fatalf("retried = %#v", retried)
	}
	if canceled.Status != StatusCanceled {
		t.Fatalf("canceled = %#v", canceled)
	}
}

func TestStore_rejects_corrupt_json_without_replacing_it(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "queue.json")
	want := []byte(`{"version":1,"jobs":[`)
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	// When
	_, err = store.List(context.Background())
	// Then
	if !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("error = %v, want ErrInvalidJSON", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(want) {
		t.Fatalf("corrupt state changed: %q", got)
	}
}

func TestStore_rejects_traversal_job_names(t *testing.T) {
	// Given
	store := newTestStore(t)
	// When
	_, err := store.Add(context.Background(), AddInput{URL: "https://example.test", Name: "../escape", OutputPath: filepath.Join(t.TempDir(), "video")})
	// Then
	if !errors.Is(err, ErrInvalidName) {
		t.Fatalf("error = %v, want ErrInvalidName", err)
	}
}

func TestStore_rejects_traversal_output_paths(t *testing.T) {
	// Given
	store := newTestStore(t)
	// When
	_, err := store.Add(context.Background(), AddInput{URL: "https://example.test", Name: "safe", OutputPath: "/tmp/../escape"})
	// Then
	if !errors.Is(err, ErrInvalidOutputPath) {
		t.Fatalf("error = %v, want ErrInvalidOutputPath", err)
	}
}

func TestStore_recovers_stale_running_job(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "queue.json")
	old := time.Now().Add(-2 * time.Hour).UTC()
	state := persistedState{Version: 1, Jobs: []Job{{ID: "job-old", URL: "https://example.test", Name: "old", OutputPath: "/tmp/old", Status: StatusRunning, Attempts: 2, CreatedAt: old, UpdatedAt: old, StartedAt: &old}}}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStoreWithOptions(path, Options{StaleAfter: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	// When
	job, err := store.Get(context.Background(), "job-old")
	if err != nil {
		t.Fatal(err)
	}
	// Then
	if job.Status != StatusQueued || job.Attempts != 2 || job.Error == "" {
		t.Fatalf("recovered job = %#v", job)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"status": "queued"`) {
		t.Fatalf("recovery was not persisted: %s", data)
	}
}

func TestStore_rejects_lock_contention(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "queue.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := path + ".lock"
	if err := os.WriteFile(lockPath, []byte("other-process"), 0o600); err != nil {
		t.Fatal(err)
	}
	// When
	_, err = store.List(context.Background())
	// Then
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("error = %v, want ErrLocked", err)
	}
}

func TestStore_writes_valid_json_state(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "queue.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	// When
	if _, err := store.Add(context.Background(), AddInput{URL: "https://example.test", Name: "safe", OutputPath: filepath.Join(t.TempDir(), "safe")}); err != nil {
		t.Fatal(err)
	}
	// Then
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded persistedState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Version != 1 || len(decoded.Jobs) != 1 || strings.Contains(string(data), ".tmp") {
		t.Fatalf("state = %s", data)
	}
	if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock cleanup error = %v", err)
	}
	temporary, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".queue-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temporary) != 0 {
		t.Fatalf("temporary files remain: %v", temporary)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}
