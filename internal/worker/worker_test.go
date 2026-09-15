package worker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"video-downloader/internal/downloader"
	"video-downloader/internal/queue"
)

func TestWorker_run_drains_jobs_without_work_on_add(t *testing.T) {
	// Given
	store := newFakeQueue(queue.Job{ID: "one", URL: "https://example.test/one", OutputPath: filepath.Join(t.TempDir(), "one"), Status: queue.StatusQueued})
	dl := &fakeDownloader{}
	worker := newTestWorker(t, store, dl, Options{Concurrency: 1})

	// When
	if err := worker.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Then
	if dl.calls() != 1 {
		t.Fatalf("download calls = %d, want 1 after explicit run", dl.calls())
	}
}

func TestWorker_run_limits_concurrency_and_persists_progress(t *testing.T) {
	// Given
	jobs := make([]queue.Job, 4)
	for index := range jobs {
		jobs[index] = queue.Job{ID: fmt.Sprintf("job-%d", index), URL: "https://example.test/video", OutputPath: filepath.Join(t.TempDir(), fmt.Sprint(index)), Status: queue.StatusQueued}
	}
	store := newFakeQueue(jobs...)
	dl := &fakeDownloader{active: make(chan struct{}, 2), progress: downloader.Progress{BytesDownloaded: 4, TotalBytes: 4, TotalBytesKnown: true, SegmentIndex: 1, SegmentTotal: 2}}
	worker := newTestWorker(t, store, dl, Options{Concurrency: 2})

	// When
	if err := worker.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Then
	if got := dl.maxActiveCount(); got > 2 {
		t.Fatalf("maximum active downloads = %d, want at most 2", got)
	}
	for _, job := range store.jobs() {
		if job.Status != queue.StatusCompleted || job.BytesDownloaded != 4 || job.BytesTotal != 4 || job.SegmentIndex != 1 || job.SegmentTotal != 2 {
			t.Fatalf("persisted job = %#v", job)
		}
	}
}

func TestWorker_run_recovers_stale_running_job(t *testing.T) {
	// Given
	old := time.Now().Add(-2 * time.Hour)
	job := queue.Job{ID: "stale", URL: "https://example.test/video", OutputPath: filepath.Join(t.TempDir(), "video"), Status: queue.StatusRunning, UpdatedAt: old}
	store := newFakeQueue(job)
	worker := newTestWorker(t, store, &fakeDownloader{}, Options{Concurrency: 1, StaleAfter: time.Hour})

	// When
	if err := worker.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Then
	got := store.job("stale")
	if got.Status != queue.StatusCompleted {
		t.Fatalf("stale job status = %s, want completed", got.Status)
	}
}

func TestWorker_run_fails_only_the_job_with_http_error(t *testing.T) {
	// Given
	failed := queue.Job{ID: "bad", URL: "https://example.test/bad", OutputPath: filepath.Join(t.TempDir(), "bad"), Status: queue.StatusQueued}
	good := queue.Job{ID: "good", URL: "https://example.test/good", OutputPath: filepath.Join(t.TempDir(), "good"), Status: queue.StatusQueued}
	store := newFakeQueue(failed, good)
	dl := &fakeDownloader{failURL: failed.URL}
	worker := newTestWorker(t, store, dl, Options{Concurrency: 2})

	// When
	err := worker.Run(context.Background())

	// Then
	if err == nil {
		t.Fatal("Run error = nil, want failed job error")
	}
	if store.job(failed.ID).Status != queue.StatusFailed || store.job(good.ID).Status != queue.StatusCompleted {
		t.Fatalf("jobs = %#v", store.jobs())
	}
}

func TestWorker_run_requeues_running_job_when_canceled(t *testing.T) {
	// Given
	ctx, cancel := context.WithCancel(context.Background())
	store := newFakeQueue(queue.Job{ID: "cancel", URL: "https://example.test/cancel", OutputPath: filepath.Join(t.TempDir(), "cancel"), Status: queue.StatusQueued})
	dl := &fakeDownloader{cancel: cancel}
	worker := newTestWorker(t, store, dl, Options{Concurrency: 1})

	// When
	err := worker.Run(ctx)

	// Then
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
	if got := store.job("cancel").Status; got != queue.StatusQueued {
		t.Fatalf("canceled job status = %s, want queued", got)
	}
}

func TestWorker_watch_stops_when_context_is_canceled_during_initial_drain(t *testing.T) {
	// Given
	ctx, cancel := context.WithCancel(context.Background())
	store := newFakeQueue(queue.Job{ID: "watch", URL: "https://example.test/watch", OutputPath: filepath.Join(t.TempDir(), "watch"), Status: queue.StatusQueued})
	dl := &fakeDownloader{cancel: cancel}
	worker := newTestWorker(t, store, dl, Options{Concurrency: 1, PollInterval: time.Hour})

	// When
	err := worker.Watch(ctx)

	// Then
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Watch error = %v, want context.Canceled", err)
	}
}

type fakeDownloader struct {
	mu        sync.Mutex
	active    chan struct{}
	activeNow int
	maxActive int
	callsNow  int
	progress  downloader.Progress
	failURL   string
	cancel    context.CancelFunc
	returnErr error
}

func (d *fakeDownloader) DownloadWithProgress(ctx context.Context, request downloader.Request, callback downloader.ProgressCallback) error {
	d.mu.Lock()
	d.callsNow++
	d.activeNow++
	if d.activeNow > d.maxActive {
		d.maxActive = d.activeNow
	}
	d.mu.Unlock()
	if d.active != nil {
		d.active <- struct{}{}
		defer func() { <-d.active }()
	}
	if d.cancel != nil {
		d.cancel()
	}
	d.mu.Lock()
	progress := d.progress
	d.mu.Unlock()
	if callback != nil && progress.BytesDownloaded > 0 {
		if err := callback(progress); err != nil {
			return err
		}
	}
	d.mu.Lock()
	d.activeNow--
	d.mu.Unlock()
	if request.Source().String() == d.failURL {
		return errors.New("HTTP 500")
	}
	if d.returnErr != nil {
		return d.returnErr
	}
	return ctx.Err()
}

func (d *fakeDownloader) calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.callsNow
}

func (d *fakeDownloader) maxActiveCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.maxActive
}

type fakeQueue struct {
	mu       sync.Mutex
	jobsByID map[string]queue.Job
}

func newFakeQueue(jobs ...queue.Job) *fakeQueue {
	items := make(map[string]queue.Job, len(jobs))
	for _, job := range jobs {
		items[job.ID] = job
	}
	return &fakeQueue{jobsByID: items}
}

func (q *fakeQueue) List(context.Context) ([]queue.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make([]queue.Job, 0, len(q.jobsByID))
	for _, job := range q.jobsByID {
		items = append(items, job)
	}
	return items, nil
}

func (q *fakeQueue) Claim(_ context.Context, id string) (queue.Job, error) {
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

func (q *fakeQueue) Update(_ context.Context, id string, progress Progress) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	job := q.jobsByID[id]
	job.BytesDownloaded = progress.BytesDownloaded
	job.BytesTotal = progress.TotalBytes
	job.SegmentIndex = progress.SegmentIndex
	job.SegmentTotal = progress.SegmentTotal
	q.jobsByID[id] = job
	return nil
}

func (q *fakeQueue) Complete(_ context.Context, id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	job := q.jobsByID[id]
	job.Status = queue.StatusCompleted
	q.jobsByID[id] = job
	return nil
}

func (q *fakeQueue) Fail(_ context.Context, id string, cause error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	job := q.jobsByID[id]
	job.Status = queue.StatusFailed
	job.Error = cause.Error()
	q.jobsByID[id] = job
	return nil
}

func (q *fakeQueue) Requeue(_ context.Context, id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	job := q.jobsByID[id]
	job.Status = queue.StatusQueued
	q.jobsByID[id] = job
	return nil
}

func (q *fakeQueue) jobs() []queue.Job {
	items, _ := q.List(context.Background())
	return items
}

func (q *fakeQueue) job(id string) queue.Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.jobsByID[id]
}

func newTestWorker(t *testing.T, store Queue, dl Downloader, options Options) *Worker {
	t.Helper()
	worker, err := New(store, dl, options)
	if err != nil {
		t.Fatal(err)
	}
	return worker
}
