package cli

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestRun_worker_flags_override_config_concurrency(t *testing.T) {
	workDir := t.TempDir()
	state := filepath.Join(workDir, "queue.json")
	configPath := filepath.Join(workDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"concurrency":1}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var active, maximum atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		current := active.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		active.Add(-1)
		_, _ = writer.Write([]byte(request.URL.Path))
	}))
	defer server.Close()
	for _, name := range []string{"one.webm", "two.webm"} {
		args := []string{"add", "--state", state, "--destination", workDir, "--name", name, server.URL + "/" + name}
		if err := Run(context.Background(), Options{Args: args, Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}, WorkingDir: workDir, Client: server.Client()}); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
	}
	workerDone := make(chan error, 1)
	go func() {
		workerDone <- Run(context.Background(), Options{Args: []string{"worker", "-c", configPath, "-s", state, "-j", "2"}, Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}, WorkingDir: workDir, Client: server.Client()})
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("worker did not start both downloads")
		}
	}
	close(release)
	if err := <-workerDone; err != nil {
		t.Fatalf("worker returned error: %v", err)
	}
	if maximum.Load() != 2 {
		t.Fatalf("maximum concurrent downloads = %d", maximum.Load())
	}
}

func TestRun_workerWatch_accepts_watch_flag(t *testing.T) {
	// Given
	workDir := t.TempDir()
	state := filepath.Join(workDir, "queue.json")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// When
	err := Run(ctx, Options{Args: []string{"worker", "--watch", "--state", state}, Out: &bytes.Buffer{}, ErrOut: &bytes.Buffer{}, WorkingDir: workDir})

	// Then
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("worker --watch returned %v, want context.Canceled", err)
	}
}
