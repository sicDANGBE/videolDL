package worker

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"video-downloader/internal/downloader"
	"video-downloader/internal/queue"
)

func TestWorker_watch_downloads_job_added_after_startup(t *testing.T) {
	// Given
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	completed := make(chan string, 1)
	store := newWatchQueue(completed)
	worker := newTestWorker(t, store, &watchDownloader{}, Options{Concurrency: 1, PollInterval: time.Millisecond})
	done := make(chan error, 1)
	go func() { done <- worker.Watch(ctx) }()
	jobID := "late"

	// When
	store.add(queue.Job{ID: jobID, URL: "https://example.test/late", OutputPath: filepath.Join(t.TempDir(), "late"), Status: queue.StatusQueued})

	// Then
	select {
	case got := <-completed:
		if got != jobID {
			t.Fatalf("completed job = %s, want %s", got, jobID)
		}
	case <-time.After(time.Second):
		t.Fatal("watch did not complete job added after startup")
	}
	cancel()
	if !errors.Is(<-done, context.Canceled) {
		t.Fatalf("Watch returned non-cancel error")
	}
}

type watchDownloader struct{}

func (watchDownloader) DownloadWithProgress(context.Context, downloader.Request, downloader.ProgressCallback) error {
	return nil
}

type watchQueue struct {
	mu        sync.Mutex
	jobsByID  map[string]queue.Job
	completed chan<- string
}

func newWatchQueue(completed chan<- string) *watchQueue {
	return &watchQueue{jobsByID: map[string]queue.Job{}, completed: completed}
}

func (q *watchQueue) List(context.Context) ([]queue.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	jobs := make([]queue.Job, 0, len(q.jobsByID))
	for _, job := range q.jobsByID {
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (q *watchQueue) Claim(_ context.Context, id string) (queue.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	job, ok := q.jobsByID[id]
	if !ok {
		return queue.Job{}, queue.ErrNotFound
	}
	job.Status = queue.StatusRunning
	q.jobsByID[id] = job
	return job, nil
}

func (q *watchQueue) Update(context.Context, string, Progress) error { return nil }

func (q *watchQueue) Complete(_ context.Context, id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	job := q.jobsByID[id]
	job.Status = queue.StatusCompleted
	q.jobsByID[id] = job
	q.completed <- id
	return nil
}

func (q *watchQueue) Fail(_ context.Context, id string, cause error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	job := q.jobsByID[id]
	job.Status = queue.StatusFailed
	job.Error = cause.Error()
	q.jobsByID[id] = job
	return nil
}

func (q *watchQueue) Requeue(_ context.Context, id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	job := q.jobsByID[id]
	job.Status = queue.StatusQueued
	q.jobsByID[id] = job
	return nil
}

func (q *watchQueue) Get(_ context.Context, id string) (queue.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	job, ok := q.jobsByID[id]
	if !ok {
		return queue.Job{}, queue.ErrNotFound
	}
	return job, nil
}

func (q *watchQueue) add(job queue.Job) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobsByID[job.ID] = job
}
