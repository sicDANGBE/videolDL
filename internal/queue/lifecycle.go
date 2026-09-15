package queue

import (
	"context"
	"fmt"
	"strings"
	"time"

	"video-downloader/internal/downloader"
)

func (store *Store) Claim(ctx context.Context, id string) (Job, error) {
	return store.change(ctx, id, func(job *Job, now time.Time) error {
		if job.Status != StatusQueued {
			return ErrInvalidTransition
		}
		job.Status = StatusRunning
		job.Attempts++
		job.LeaseID = newID(now)
		job.StartedAt = &now
		job.Error = ""
		job.UpdatedAt = now
		return nil
	})
}

func (store *Store) Update(ctx context.Context, id string, progress downloader.Progress) error {
	_, err := store.change(ctx, id, func(job *Job, now time.Time) error {
		if job.Status != StatusRunning {
			return ErrInvalidTransition
		}
		job.BytesDownloaded = progress.BytesDownloaded
		if progress.TotalBytesKnown {
			job.BytesTotal = progress.TotalBytes
		}
		job.SegmentIndex = progress.SegmentIndex
		job.SegmentTotal = progress.SegmentTotal
		job.UpdatedAt = now
		return nil
	})
	return err
}

func (store *Store) UpdateOwned(ctx context.Context, id, lease string, progress downloader.Progress) error {
	_, err := store.change(ctx, id, func(job *Job, now time.Time) error {
		if err := requireLease(job, lease); err != nil {
			return err
		}
		if job.Status != StatusRunning {
			return ErrInvalidTransition
		}
		job.BytesDownloaded = progress.BytesDownloaded
		if progress.TotalBytesKnown {
			job.BytesTotal = progress.TotalBytes
		}
		job.SegmentIndex = progress.SegmentIndex
		job.SegmentTotal = progress.SegmentTotal
		job.UpdatedAt = now
		return nil
	})
	return err
}

func (store *Store) Complete(ctx context.Context, id string) error {
	_, err := store.change(ctx, id, func(job *Job, now time.Time) error {
		if job.Status != StatusRunning {
			return ErrInvalidTransition
		}
		job.Status = StatusCompleted
		job.LeaseID = ""
		job.CompletedAt = &now
		job.UpdatedAt = now
		return nil
	})
	return err
}

func (store *Store) CompleteOwned(ctx context.Context, id, lease string) error {
	_, err := store.change(ctx, id, func(job *Job, now time.Time) error {
		if err := requireLease(job, lease); err != nil {
			return err
		}
		if job.Status != StatusRunning {
			return ErrInvalidTransition
		}
		job.Status = StatusCompleted
		job.LeaseID = ""
		job.CompletedAt = &now
		job.UpdatedAt = now
		return nil
	})
	return err
}

func (store *Store) Fail(ctx context.Context, id string, cause error) error {
	if cause == nil {
		return fmt.Errorf("fail job: nil cause")
	}
	_, err := store.change(ctx, id, func(job *Job, now time.Time) error {
		if job.Status != StatusRunning {
			return ErrInvalidTransition
		}
		job.Status = StatusFailed
		job.LeaseID = ""
		job.Error = redactError(job.URL, cause.Error())
		job.UpdatedAt = now
		return nil
	})
	return err
}

func (store *Store) FailOwned(ctx context.Context, id, lease string, cause error) error {
	if cause == nil {
		return fmt.Errorf("fail job: nil cause")
	}
	_, err := store.change(ctx, id, func(job *Job, now time.Time) error {
		if err := requireLease(job, lease); err != nil {
			return err
		}
		if job.Status != StatusRunning {
			return ErrInvalidTransition
		}
		job.Status = StatusFailed
		job.LeaseID = ""
		job.Error = redactError(job.URL, cause.Error())
		job.UpdatedAt = now
		return nil
	})
	return err
}

func redactError(source, message string) string {
	if source == "" {
		return message
	}
	return strings.ReplaceAll(message, source, "[source redacted]")
}

func (store *Store) Requeue(ctx context.Context, id string) error {
	_, err := store.change(ctx, id, func(job *Job, now time.Time) error {
		if job.Status != StatusRunning {
			return ErrInvalidTransition
		}
		job.Status = StatusQueued
		job.LeaseID = ""
		job.StartedAt = nil
		job.UpdatedAt = now
		return nil
	})
	return err
}

func (store *Store) RequeueOwned(ctx context.Context, id, lease string) error {
	_, err := store.change(ctx, id, func(job *Job, now time.Time) error {
		if err := requireLease(job, lease); err != nil {
			return err
		}
		if job.Status != StatusRunning {
			return ErrInvalidTransition
		}
		job.Status = StatusQueued
		job.LeaseID = ""
		job.StartedAt = nil
		job.UpdatedAt = now
		return nil
	})
	return err
}

func requireLease(job *Job, lease string) error {
	if lease == "" || job.LeaseID != lease {
		return ErrInvalidTransition
	}
	return nil
}

func (store *Store) change(ctx context.Context, id string, update func(*Job, time.Time) error) (Job, error) {
	lock, err := acquireLock(ctx, store.lockPath)
	if err != nil {
		return Job{}, err
	}
	state, err := readState(store.path)
	if err == nil {
		state, _, err = store.recoverStale(state)
	}
	if err != nil {
		return Job{}, store.closeWithError(lock, err)
	}
	for index := range state.Jobs {
		if state.Jobs[index].ID != id {
			continue
		}
		job := &state.Jobs[index]
		if err := update(job, store.now().UTC()); err != nil {
			return Job{}, store.closeWithError(lock, err)
		}
		if err := store.writeState(store.path, state); err != nil {
			return Job{}, store.closeWithError(lock, err)
		}
		return *job, store.closeWithError(lock, nil)
	}
	return Job{}, store.closeWithError(lock, ErrNotFound)
}
