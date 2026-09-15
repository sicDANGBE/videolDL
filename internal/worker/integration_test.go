package worker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"video-downloader/internal/downloader"
	"video-downloader/internal/queue"
)

func TestWorker_realQueue_downloads_exact_bytes_and_add_does_not_fetch(t *testing.T) {
	// Given
	var requests atomic.Int32
	want := []byte("exact-video-bytes")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(want)
	}))
	defer server.Close()
	directory := t.TempDir()
	store, err := queue.NewStore(filepath.Join(directory, "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "video.bin")
	if _, err := store.Add(context.Background(), queue.AddInput{URL: server.URL + "/video", Name: "video", OutputPath: output}); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 0 {
		t.Fatalf("requests after add = %d, want 0", requests.Load())
	}
	worker, err := New(store, downloader.New(server.Client(), ""), Options{Concurrency: 1})
	if err != nil {
		t.Fatal(err)
	}

	// When
	if err := worker.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Then
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) || requests.Load() != 1 {
		t.Fatalf("output = %q, requests = %d", got, requests.Load())
	}
}

func TestWorker_realQueue_never_exceeds_two_active_requests(t *testing.T) {
	// Given
	var active atomic.Int32
	var maximum atomic.Int32
	ready := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		current := active.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		if current == 2 {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
		<-release
		active.Add(-1)
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("ok"))
	}))
	defer server.Close()
	directory := t.TempDir()
	store, err := queue.NewStore(filepath.Join(directory, "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 4; index++ {
		if _, err := store.Add(context.Background(), queue.AddInput{URL: server.URL + "/video", Name: string(rune('a' + index)), OutputPath: filepath.Join(directory, string(rune('a'+index)))}); err != nil {
			t.Fatal(err)
		}
	}
	worker, err := New(store, downloader.New(server.Client(), ""), Options{Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}

	// When
	done := make(chan error, 1)
	go func() { done <- worker.Run(context.Background()) }()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("two requests did not become active")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	// Then
	if maximum.Load() > 2 {
		t.Fatalf("maximum active requests = %d", maximum.Load())
	}
}

func TestWorker_realQueue_http_error_fails_only_one_job(t *testing.T) {
	// Given
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/bad" {
			response.WriteHeader(http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("good"))
	}))
	defer server.Close()
	directory := t.TempDir()
	store, err := queue.NewStore(filepath.Join(directory, "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	bad, err := store.Add(context.Background(), queue.AddInput{URL: server.URL + "/bad", Name: "bad", OutputPath: filepath.Join(directory, "bad")})
	if err != nil {
		t.Fatal(err)
	}
	good, err := store.Add(context.Background(), queue.AddInput{URL: server.URL + "/good", Name: "good", OutputPath: filepath.Join(directory, "good")})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := New(store, downloader.New(server.Client(), ""), Options{Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}

	// When
	err = worker.Run(context.Background())

	// Then
	if err == nil {
		t.Fatal("Run error = nil, want HTTP failure")
	}
	badJob, badErr := store.Get(context.Background(), bad.ID)
	goodJob, goodErr := store.Get(context.Background(), good.ID)
	if badErr != nil || goodErr != nil || badJob.Status != queue.StatusFailed || goodJob.Status != queue.StatusCompleted {
		t.Fatalf("bad = %#v (%v), good = %#v (%v)", badJob, badErr, goodJob, goodErr)
	}
}

func TestWorker_realQueue_cancellation_requeues_job(t *testing.T) {
	// Given
	started := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		once.Do(func() { close(started) })
		<-request.Context().Done()
	}))
	defer server.Close()
	directory := t.TempDir()
	store, err := queue.NewStore(filepath.Join(directory, "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.Add(context.Background(), queue.AddInput{URL: server.URL + "/video", Name: "video", OutputPath: filepath.Join(directory, "video")})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := New(store, downloader.New(server.Client(), ""), Options{Concurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}

	// When
	cancel()
	err = <-done

	// Then
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
	got, err := store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != queue.StatusQueued {
		t.Fatalf("canceled job status = %s, want queued", got.Status)
	}
}

func TestWorker_realQueue_live_context_downloader_cancellation_requeues_with_exact_state(t *testing.T) {
	// Given
	fixed := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	directory := t.TempDir()
	store, err := queue.NewStoreWithOptions(filepath.Join(directory, "queue.json"), queue.Options{Now: func() time.Time { return fixed }})
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.Add(context.Background(), queue.AddInput{URL: "https://example.test/video", Name: "video", OutputPath: filepath.Join(directory, "video")})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := New(store, &fakeDownloader{returnErr: context.Canceled}, Options{Concurrency: 1, Now: func() time.Time { return fixed }})
	if err != nil {
		t.Fatal(err)
	}

	// When
	err = worker.Run(context.Background())

	// Then
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
	got, err := store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != queue.StatusQueued || got.Attempts != 1 || got.StartedAt != nil || got.CompletedAt != nil || got.Error != "" {
		t.Fatalf("requeued state = %#v", got)
	}
	if !got.UpdatedAt.Equal(fixed) || !got.CreatedAt.Equal(fixed) {
		t.Fatalf("timestamps = created %s updated %s, want %s", got.CreatedAt, got.UpdatedAt, fixed)
	}
}

func TestWorker_realQueue_cancellation_during_dispatch_does_not_claim_job(t *testing.T) {
	// Given
	directory := t.TempDir()
	store, err := queue.NewStore(filepath.Join(directory, "queue.json"))
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.Add(context.Background(), queue.AddInput{URL: "https://example.test/video", Name: "video", OutputPath: filepath.Join(directory, "video")})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := New(store, &fakeDownloader{}, Options{Concurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// When
	err = worker.Run(ctx)

	// Then
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
	got, err := store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != queue.StatusQueued || got.Attempts != 0 || got.StartedAt != nil || got.Error != "" {
		t.Fatalf("dispatch-canceled state = %#v", got)
	}
}
