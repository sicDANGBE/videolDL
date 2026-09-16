package worker

import (
	"context"
	"errors"
	"fmt"
	"time"
	"video-downloader/internal/queue"
)

func (w *Worker) Run(ctx context.Context) error   { return w.supervise(ctx, false) }
func (w *Worker) Watch(ctx context.Context) error { return w.supervise(ctx, true) }

type jobResult struct {
	id  string
	err error
}

// A single event loop owns scheduling and checks all cancellations together.
// Run consumes its initial snapshot; Watch admits new jobs on each refresh.
func (w *Worker) supervise(ctx context.Context, watch bool) error {
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	ticker := time.NewTicker(w.options.PollInterval)
	defer ticker.Stop()
	var jobs []queue.Job
	for {
		var err error
		jobs, err = w.list(runCtx)
		if err == nil {
			break
		}
		if !watch || !errors.Is(err, queue.ErrLocked) {
			return fmt.Errorf("list jobs: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	for _, job := range jobs {
		if job.Status == queue.StatusRunning && w.isStale(job) {
			if err := w.requeue(runCtx, job.ID, job.LeaseID); err != nil {
				return err
			}
		}
	}
	var err error
	jobs, err = w.snapshot(runCtx)
	if err != nil {
		return err
	}
	initial := map[string]bool{}
	for _, job := range jobs {
		if job.Status == queue.StatusQueued {
			initial[job.ID] = true
		}
	}
	attempted := map[string]bool{}
	active := map[string]context.CancelCauseFunc{}
	results := make(chan jobResult, w.options.Concurrency)
	var jobErrors []error
	// Do not release the supervisor lock while transfers are still shutting down.
	defer func() {
		stop()
		for len(active) > 0 {
			result := <-results
			delete(active, result.id)
		}
	}()
	refresh := func() error {
		snapshot, err := w.snapshot(runCtx)
		if err != nil {
			return err
		}
		jobs = snapshot
		states := map[string]queue.Status{}
		for _, job := range jobs {
			states[job.ID] = job.Status
		}
		for id, cancel := range active {
			state, exists := states[id]
			if !exists || state == queue.StatusCanceled {
				cancel(errJobCanceled)
			}
		}
		return nil
	}
	for {
		if ctx.Err() != nil {
			return errors.Join(ctx.Err(), errors.Join(jobErrors...))
		}
		for _, job := range jobs {
			if len(active) >= w.options.Concurrency {
				break
			}
			if job.Status != queue.StatusQueued || active[job.ID] != nil || (!watch && (!initial[job.ID] || attempted[job.ID])) {
				continue
			}
			taskCtx, cancel := context.WithCancelCause(runCtx)
			active[job.ID] = cancel
			attempted[job.ID] = true
			go func(job queue.Job) { results <- jobResult{id: job.ID, err: w.process(taskCtx, job)} }(job)
		}
		if !watch && len(active) == 0 {
			return errors.Join(jobErrors...)
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), errors.Join(jobErrors...))
		case result := <-results:
			active[result.id](nil)
			delete(active, result.id)
			if result.err != nil {
				if watch {
					if !watchCanContinue(result.err) {
						return result.err
					}
				} else {
					jobErrors = append(jobErrors, result.err)
				}
			}
			if err := refresh(); err != nil {
				if !errors.Is(err, queue.ErrLocked) {
					return err
				}
				jobs = nil
			}
		case <-ticker.C:
			if err := refresh(); err != nil {
				if !errors.Is(err, queue.ErrLocked) {
					return err
				}
				jobs = nil
			}
		}
	}
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
