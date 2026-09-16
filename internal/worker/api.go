package worker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"video-downloader/internal/downloader"
	"video-downloader/internal/queue"
)

type Progress = downloader.Progress

type Queue interface {
	List(context.Context) ([]queue.Job, error)
	Claim(context.Context, string) (queue.Job, error)
	Update(context.Context, string, Progress) error
	Complete(context.Context, string) error
	Fail(context.Context, string, error) error
	Requeue(context.Context, string) error
}

type OwnedQueue interface {
	UpdateOwned(context.Context, string, string, Progress) error
	CompleteOwned(context.Context, string, string) error
	FailOwned(context.Context, string, string, error) error
	RequeueOwned(context.Context, string, string) error
}

type Downloader interface {
	DownloadWithProgress(context.Context, downloader.Request, downloader.ProgressCallback) error
}

type Options struct {
	Concurrency  int
	StaleAfter   time.Duration
	PollInterval time.Duration
	Now          func() time.Time
}

type Worker struct {
	queue      Queue
	downloader Downloader
	options    Options
	storeMu    sync.Mutex
}

func New(store Queue, dl Downloader, options Options) (*Worker, error) {
	if store == nil || dl == nil {
		return nil, ErrInvalidDependency
	}
	if options.Concurrency == 0 {
		options.Concurrency = 1
	}
	if options.Concurrency < 1 || options.Concurrency > 8 {
		return nil, ErrInvalidConcurrency
	}
	if options.StaleAfter <= 0 {
		options.StaleAfter = 24 * time.Hour
	}
	if options.PollInterval <= 0 {
		options.PollInterval = time.Second
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Worker{queue: store, downloader: dl, options: options}, nil
}

func (w *Worker) list(ctx context.Context) ([]queue.Job, error) {
	w.storeMu.Lock()
	defer w.storeMu.Unlock()
	return w.queue.List(ctx)
}

func (w *Worker) get(ctx context.Context, id string) (queue.Job, error) {
	w.storeMu.Lock()
	defer w.storeMu.Unlock()
	reader, ok := w.queue.(interface {
		Get(context.Context, string) (queue.Job, error)
	})
	if !ok {
		return queue.Job{}, fmt.Errorf("queue does not support job reads")
	}
	return reader.Get(ctx, id)
}

func (w *Worker) claim(ctx context.Context, id string) (queue.Job, error) {
	w.storeMu.Lock()
	defer w.storeMu.Unlock()
	return w.queue.Claim(ctx, id)
}

func (w *Worker) update(ctx context.Context, id, lease string, progress Progress) error {
	w.storeMu.Lock()
	defer w.storeMu.Unlock()
	if owned, ok := w.queue.(OwnedQueue); ok {
		return owned.UpdateOwned(ctx, id, lease, progress)
	}
	return w.queue.Update(ctx, id, progress)
}

func (w *Worker) complete(ctx context.Context, id, lease string) error {
	w.storeMu.Lock()
	defer w.storeMu.Unlock()
	if owned, ok := w.queue.(OwnedQueue); ok {
		return owned.CompleteOwned(ctx, id, lease)
	}
	return w.queue.Complete(ctx, id)
}

func (w *Worker) failJob(ctx context.Context, id, lease string, cause error) error {
	w.storeMu.Lock()
	defer w.storeMu.Unlock()
	if owned, ok := w.queue.(OwnedQueue); ok {
		return owned.FailOwned(ctx, id, lease, cause)
	}
	return w.queue.Fail(ctx, id, cause)
}

func (w *Worker) requeue(ctx context.Context, id, lease string) error {
	w.storeMu.Lock()
	defer w.storeMu.Unlock()
	if lease != "" {
		if owned, ok := w.queue.(OwnedQueue); ok {
			return owned.RequeueOwned(ctx, id, lease)
		}
	}
	return w.queue.Requeue(ctx, id)
}

func (w *Worker) snapshot(ctx context.Context) ([]queue.Job, error) {
	if reader, ok := w.queue.(interface {
		Snapshot(context.Context) ([]queue.Job, error)
	}); ok {
		return reader.Snapshot(ctx)
	}
	return w.list(ctx)
}
