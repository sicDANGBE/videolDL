package worker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	"video-downloader/internal/downloader"
	"video-downloader/internal/queue"
)

type controlledDownload struct {
	started chan string
	release chan struct{}
}

func (d *controlledDownload) DownloadWithProgress(ctx context.Context, r downloader.Request, _ downloader.ProgressCallback) error {
	d.started <- filepath.Base(r.OutputPath())
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-d.release:
		return nil
	}
}

type measuredQueue struct {
	*fakeQueue
	snapshots, gets atomic.Int64
}

func (q *measuredQueue) Snapshot(ctx context.Context) ([]queue.Job, error) {
	q.snapshots.Add(1)
	return q.fakeQueue.List(ctx)
}
func (q *measuredQueue) Get(_ context.Context, id string) (queue.Job, error) {
	q.gets.Add(1)
	return q.job(id), nil
}
func receiveStart(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case name := <-ch:
		return name
	case <-time.After(3 * time.Second):
		t.Fatal("available slot was not filled")
		return ""
	}
}
func superviseTest(t *testing.T, w *Worker, watch bool) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		if watch {
			done <- w.Watch(ctx)
		} else {
			done <- w.Run(ctx)
		}
	}()
	t.Cleanup(cancel)
	return cancel, done
}
func waitSupervisor(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not stop")
	}
}
func TestContinuousAdmissionAndSingleSharedCancellationRead(t *testing.T) {
	q := &measuredQueue{fakeQueue: newFakeQueue()}
	add := func(id string) {
		q.mu.Lock()
		defer q.mu.Unlock()
		q.jobsByID[id] = queue.Job{ID: id, URL: "https://example.test/video", OutputPath: filepath.Join(t.TempDir(), id), Status: queue.StatusQueued}
	}
	add("long")
	d := &controlledDownload{started: make(chan string, 10), release: make(chan struct{})}
	w := newTestWorker(t, q, d, Options{Concurrency: 8, PollInterval: 30 * time.Millisecond})
	cancel, done := superviseTest(t, w, true)
	if got := receiveStart(t, d.started); got != "long" {
		t.Fatal(got)
	}
	// Fill seven vacant slots while the first transfer is deliberately blocked.
	for i := 1; i < 8; i++ {
		add(fmt.Sprint(i))
	}
	for i := 1; i < 8; i++ {
		receiveStart(t, d.started)
	}
	before := q.snapshots.Load()
	time.Sleep(180 * time.Millisecond)
	reads := q.snapshots.Load() - before
	if reads < 1 || reads > 12 {
		t.Fatalf("snapshot reads=%d for eight active transfers", reads)
	}
	if q.gets.Load() != 0 {
		t.Fatal("per-job polling during transfers")
	}
	add("ninth")
	select {
	case id := <-d.started:
		t.Fatalf("ninth transfer started above the cap: %s", id)
	case <-time.After(90 * time.Millisecond):
	}
	// Cancel one transfer through the shared snapshot. The ninth must take its slot.
	q.mu.Lock()
	job := q.jobsByID["long"]
	job.Status = queue.StatusCanceled
	q.jobsByID["long"] = job
	q.mu.Unlock()
	if got := receiveStart(t, d.started); got != "ninth" {
		t.Fatal(got)
	}
	if q.job("long").Status != queue.StatusCanceled {
		t.Fatal("canceled task was requeued")
	}
	cancel()
	waitSupervisor(t, done)
	for _, job := range q.jobs() {
		if job.ID != "long" && job.Status != queue.StatusQueued {
			t.Fatalf("shutdown did not requeue %s: %s", job.ID, job.Status)
		}
	}
}
func TestOneShotLeavesLaterAdditionsQueued(t *testing.T) {
	dir := t.TempDir()
	q := &measuredQueue{fakeQueue: newFakeQueue(queue.Job{ID: "first", URL: "https://example.test/video", OutputPath: filepath.Join(dir, "first"), Status: queue.StatusQueued})}
	d := &controlledDownload{started: make(chan string, 2), release: make(chan struct{})}
	w := newTestWorker(t, q, d, Options{Concurrency: 2, PollInterval: 10 * time.Millisecond})
	_, done := superviseTest(t, w, false)
	receiveStart(t, d.started)
	q.mu.Lock()
	q.jobsByID["late"] = queue.Job{ID: "late", URL: "https://example.test/video", OutputPath: filepath.Join(dir, "late"), Status: queue.StatusQueued}
	q.mu.Unlock()
	close(d.release)
	waitSupervisor(t, done)
	if q.job("late").Status != queue.StatusQueued {
		t.Fatal("one-shot consumed a later addition")
	}
}
