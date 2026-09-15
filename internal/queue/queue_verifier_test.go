package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestStore_rejects_invalid_persisted_schema_on_list_and_get(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{name: "unknown status", json: `{"version":1,"jobs":[{"id":"job-1","url":"https://example.test","name":"safe","output_path":"/tmp/safe","status":"paused","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]}`},
		{name: "missing output path", json: `{"version":1,"jobs":[{"id":"job-1","url":"https://example.test","name":"safe","status":"queued","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			path := filepath.Join(t.TempDir(), "queue.json")
			if err := os.WriteFile(path, []byte(test.json), 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := NewStore(path)
			if err != nil {
				t.Fatal(err)
			}
			// When
			_, err = store.List(context.Background())
			// Then
			var schemaErr *JobSchemaError
			if !errors.As(err, &schemaErr) || !errors.Is(err, ErrInvalidJobSchema) {
				t.Fatalf("list error = %v, want typed schema error", err)
			}
			_, err = store.Get(context.Background(), "job-1")
			if !errors.As(err, &schemaErr) || !errors.Is(err, ErrInvalidJobSchema) {
				t.Fatalf("get error = %v, want typed schema error", err)
			}
		})
	}
}

func TestStore_preserves_state_when_atomic_write_fails(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "queue.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add(context.Background(), AddInput{URL: "https://example.test/old", Name: "old", OutputPath: "/tmp/old"}); err != nil {
		t.Fatal(err)
	}
	prior, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	store.writeState = func(statePath string, state persistedState) error {
		return writeStateWithRename(statePath, state, func(_, _ string) error {
			return errors.New("injected rename failure")
		})
	}
	// When
	_, err = store.Add(context.Background(), AddInput{URL: "https://example.test/new", Name: "new", OutputPath: "/tmp/new"})
	// Then
	if err == nil || !strings.Contains(err.Error(), "injected rename failure") {
		t.Fatalf("error = %v, want injected failure", err)
	}
	current, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(current) != string(prior) {
		t.Fatal("prior valid state was replaced after failed atomic write")
	}
	assertQueueArtifactsRemoved(t, path)
}

func TestStore_rejects_retry_and_cancel_for_completed_job(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "queue.json")
	state := `{"version":1,"jobs":[{"id":"job-done","url":"https://example.test","name":"done","output_path":"/tmp/done","status":"completed","attempts":1,"bytes_downloaded":4,"bytes_total":4,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(state), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	// When
	retryErr := store.Retry(context.Background(), "job-done")
	cancelErr := store.Cancel(context.Background(), "job-done")
	// Then
	if !errors.Is(retryErr, ErrInvalidTransition) || !errors.Is(cancelErr, ErrInvalidTransition) {
		t.Fatalf("retry = %v, cancel = %v, want invalid transitions", retryErr, cancelErr)
	}
}

func TestStore_concurrent_writers_keep_state_valid(t *testing.T) {
	// Given
	path := filepath.Join(t.TempDir(), "queue.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 8)
	var writers sync.WaitGroup
	for index := 0; index < 8; index++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			<-start
			_, writeErr := store.Add(context.Background(), AddInput{URL: fmt.Sprintf("https://example.test/%d", index), Name: fmt.Sprintf("video-%d", index), OutputPath: fmt.Sprintf("/tmp/video-%d", index)})
			results <- writeErr
		}()
	}
	// When
	close(start)
	writers.Wait()
	close(results)
	// Then
	successes := 0
	for writeErr := range results {
		if writeErr == nil {
			successes++
			continue
		}
		if !errors.Is(writeErr, ErrLocked) {
			t.Fatalf("concurrent writer error = %v", writeErr)
		}
	}
	if successes == 0 {
		t.Fatal("no concurrent writer acquired the lock")
	}
	items, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if len(items) != successes || len(state.Jobs) != successes {
		t.Fatalf("items = %d, state jobs = %d, successes = %d", len(items), len(state.Jobs), successes)
	}
	assertQueueArtifactsRemoved(t, path)
}

func assertQueueArtifactsRemoved(t *testing.T, path string) {
	t.Helper()
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
