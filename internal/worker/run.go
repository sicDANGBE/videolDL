package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"video-downloader/internal/queue"
)

func (w *Worker) Run(ctx context.Context) error {
	return w.runOnce(ctx)
}

func (w *Worker) Watch(ctx context.Context) error {
	if err := w.runOnce(ctx); err != nil && !watchCanContinue(err) {
		return err
	}
	ticker := time.NewTicker(w.options.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := w.runOnce(ctx); err != nil && !watchCanContinue(err) {
				return err
			}
		}
	}
}

func (w *Worker) runOnce(ctx context.Context) error {
	jobs, err := w.list(ctx)
	if err != nil {
		return fmt.Errorf("list jobs: %w", err)
	}
	for _, job := range jobs {
		if job.Status == queue.StatusRunning && w.isStale(job) {
			if err := w.requeue(ctx, job.ID, job.LeaseID); err != nil {
				return fmt.Errorf("recover job %s: %w", job.ID, err)
			}
		}
	}
	jobs, err = w.list(ctx)
	if err != nil {
		return fmt.Errorf("list recoverable jobs: %w", err)
	}
	work := make(chan queue.Job)
	var group sync.WaitGroup
	var errorsMu sync.Mutex
	var runErrors []error
	workerCount := w.options.Concurrency
	if workerCount > len(jobs) {
		workerCount = len(jobs)
	}
	for index := 0; index < workerCount; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-work:
					if !ok {
						return
					}
					if job.Status != queue.StatusQueued {
						continue
					}
					if err := w.process(ctx, job); err != nil {
						errorsMu.Lock()
						runErrors = append(runErrors, err)
						errorsMu.Unlock()
					}
				}
			}
		}()
	}
	for _, job := range jobs {
		select {
		case work <- job:
		case <-ctx.Done():
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(work)
	group.Wait()
	if ctx.Err() != nil {
		runErrors = append(runErrors, ctx.Err())
	}
	return errors.Join(runErrors...)
}

func (w *Worker) isStale(job queue.Job) bool {
	return !job.UpdatedAt.IsZero() && w.options.Now().Sub(job.UpdatedAt) > w.options.StaleAfter
}

func watchCanContinue(err error) bool {
	if onlyJobErrors(err) {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if !watchCanContinue(child) {
				return false
			}
		}
		return true
	}
	return errors.Is(err, queue.ErrLocked)
}
