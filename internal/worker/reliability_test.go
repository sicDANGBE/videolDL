package worker

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	"video-downloader/internal/downloader"
	"video-downloader/internal/queue"
)

func TestWatchContinuesAfterFailedJob(t *testing.T) {
	store := newFakeQueue(queue.Job{ID: "bad", URL: "https://example.test/bad", OutputPath: filepath.Join(t.TempDir(), "bad"), Status: queue.StatusQueued})
	worker := newTestWorker(t, store, &fakeDownloader{failURL: "https://example.test/bad"}, Options{PollInterval: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- worker.Watch(ctx) }()
	deadline := time.Now().Add(time.Second)
	for store.job("bad").Status != queue.StatusFailed && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	store.mu.Lock()
	store.jobsByID["later"] = queue.Job{ID: "later", URL: "https://example.test/good", OutputPath: filepath.Join(t.TempDir(), "good"), Status: queue.StatusQueued}
	store.mu.Unlock()
	for store.job("later").Status != queue.StatusCompleted && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if store.job("later").Status != queue.StatusCompleted {
		t.Fatal("watch stopped after failed job")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

type countingQueue struct {
	*fakeQueue
	updates atomic.Int32
}

func (q *countingQueue) Update(ctx context.Context, id string, p Progress) error {
	q.updates.Add(1)
	return q.fakeQueue.Update(ctx, id, p)
}

type burstDownloader struct{}

func (burstDownloader) DownloadWithProgress(ctx context.Context, r downloader.Request, progress downloader.ProgressCallback) error {
	for i := 1; i <= 1000; i++ {
		if err := progress(Progress{BytesDownloaded: int64(i)}); err != nil {
			return err
		}
	}
	return nil
}
func TestProgressWritesAreBatchedAndFinalCountIsSaved(t *testing.T) {
	store := &countingQueue{fakeQueue: newFakeQueue(queue.Job{ID: "burst", URL: "https://example.test/video", OutputPath: filepath.Join(t.TempDir(), "video"), Status: queue.StatusQueued})}
	now := time.Now()
	w := newTestWorker(t, store, burstDownloader{}, Options{Now: func() time.Time { return now }})
	if err := w.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.updates.Load() != 2 || store.job("burst").BytesDownloaded != 1000 {
		t.Fatalf("updates=%d, bytes=%d", store.updates.Load(), store.job("burst").BytesDownloaded)
	}
}
func TestMixedQueueAndJobErrorsRemainFatal(t *testing.T) {
	if onlyJobErrors(errors.Join(&jobError{errors.New("download")}, errors.New("state write"))) {
		t.Fatal("infrastructure error suppressed")
	}
}
