package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"video-downloader/internal/downloader"
	"video-downloader/internal/queue"
)

func (w *Worker) process(ctx context.Context, listed queue.Job) error {
	job, err := w.claim(ctx, listed.ID)
	if err != nil {
		if errors.Is(err, queue.ErrInvalidTransition) || errors.Is(err, queue.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("claim job %s: %w", listed.ID, err)
	}
	request, err := downloader.NewRequest(job.URL, job.OutputPath, false)
	if err != nil {
		return w.fail(ctx, job.ID, job.LeaseID, fmt.Errorf("build request: %w", err))
	}
	request = request.WithOptions(job.DownloadOptions)
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go watchCancellation(workCtx, stopWatch, cancel, w.get, job.ID)
	var lastSaved time.Time
	var latest downloader.Progress
	var dirty bool
	progress := func(value downloader.Progress) error {
		latest, dirty = value, true
		if !lastSaved.IsZero() && w.options.Now().Sub(lastSaved) < time.Second {
			return nil
		}
		if err := w.update(workCtx, job.ID, job.LeaseID, value); err != nil {
			return err
		}
		lastSaved, dirty = w.options.Now(), false
		return nil
	}
	err = w.downloader.DownloadWithProgress(workCtx, request, progress)
	if dirty && !w.isCanceledJob(context.WithoutCancel(ctx), job.ID) {
		if saveErr := w.update(context.WithoutCancel(ctx), job.ID, job.LeaseID, latest); saveErr != nil {
			return errors.Join(err, fmt.Errorf("persist final progress: %w", saveErr))
		}
	}
	if err != nil {
		if ctx.Err() == nil && w.isCanceledJob(context.WithoutCancel(ctx), job.ID) {
			return &jobError{err}
		}
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			cancellationErr := err
			if ctxErr := ctx.Err(); ctxErr != nil {
				cancellationErr = ctxErr
			}
			if requeueErr := w.requeue(context.WithoutCancel(ctx), job.ID, job.LeaseID); requeueErr != nil {
				return errors.Join(cancellationErr, fmt.Errorf("requeue canceled job %s: %w", job.ID, requeueErr))
			}
			return cancellationErr
		}
		return w.fail(ctx, job.ID, job.LeaseID, err)
	}
	if err := w.complete(ctx, job.ID, job.LeaseID); err != nil {
		return fmt.Errorf("complete job %s: %w", job.ID, err)
	}
	return nil
}

func watchCancellation(ctx context.Context, stop <-chan struct{}, cancel context.CancelFunc, get func(context.Context, string) (queue.Job, error), id string) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			job, err := get(context.WithoutCancel(ctx), id)
			if err == nil && job.Status == queue.StatusCanceled {
				cancel()
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (w *Worker) isCanceledJob(ctx context.Context, id string) bool {
	job, err := w.get(ctx, id)
	return err == nil && job.Status == queue.StatusCanceled
}

func (w *Worker) fail(ctx context.Context, id, lease string, cause error) error {
	if err := w.failJob(ctx, id, lease, cause); err != nil {
		return errors.Join(cause, fmt.Errorf("fail job %s: %w", id, err))
	}
	return &jobError{cause}
}

// jobError is already recorded on its job; it must not stop supervision.
type jobError struct{ error }

func (e *jobError) Unwrap() error { return e.error }
func onlyJobErrors(err error) bool {
	if err == nil {
		return true
	}
	if _, ok := err.(*jobError); ok {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if !onlyJobErrors(child) {
				return false
			}
		}
		return true
	}
	return false
}
