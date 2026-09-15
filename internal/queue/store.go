package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Store struct {
	path       string
	lockPath   string
	staleAfter time.Duration
	now        func() time.Time
	writeState func(string, persistedState) error
}

func NewStore(path string) (*Store, error) {
	return NewStoreWithOptions(path, Options{})
}

func NewStoreWithOptions(path string, options Options) (*Store, error) {
	if path == "" || strings.ContainsRune(path, 0) {
		return nil, fmt.Errorf("queue state path is invalid")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create queue state directory: %w", err)
	}
	if options.StaleAfter <= 0 {
		options.StaleAfter = 24 * time.Hour
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Store{path: path, lockPath: path + ".lock", staleAfter: options.StaleAfter, now: options.Now, writeState: writeState}, nil
}

func (store *Store) Add(ctx context.Context, input AddInput) (Job, error) {
	if err := validateInput(input); err != nil {
		return Job{}, err
	}
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
	for _, existing := range state.Jobs {
		if existing.OutputPath == input.OutputPath && (existing.Status == StatusQueued || existing.Status == StatusRunning) {
			return Job{}, store.closeWithError(lock, fmt.Errorf("destination already reserved by job %s", existing.ID))
		}
	}
	if _, err := os.Lstat(input.OutputPath); err == nil {
		return Job{}, store.closeWithError(lock, fmt.Errorf("destination already exists; choose another name"))
	} else if !os.IsNotExist(err) {
		return Job{}, store.closeWithError(lock, err)
	}
	now := store.now().UTC()
	job := Job{DownloadOptions: input.DownloadOptions, ID: newID(now), URL: input.URL, Name: input.Name, OutputPath: input.OutputPath, Status: StatusQueued, CreatedAt: now, UpdatedAt: now}
	state.Jobs = append(state.Jobs, job)
	if err := store.writeState(store.path, state); err != nil {
		return Job{}, store.closeWithError(lock, err)
	}
	return job, store.closeWithError(lock, nil)
}

func (store *Store) List(ctx context.Context) ([]Job, error) {
	lock, err := acquireLock(ctx, store.lockPath)
	if err != nil {
		return nil, err
	}
	state, err := readState(store.path)
	changed := false
	if err == nil {
		state, changed, err = store.recoverStale(state)
	}
	if err != nil {
		return nil, store.closeWithError(lock, err)
	}
	if changed {
		if err := store.writeState(store.path, state); err != nil {
			return nil, store.closeWithError(lock, err)
		}
	}
	if err := validateState(state); err != nil {
		return nil, store.closeWithError(lock, err)
	}
	items := append([]Job(nil), state.Jobs...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	return items, store.closeWithError(lock, nil)
}

func (store *Store) Get(ctx context.Context, id string) (Job, error) {
	lock, err := acquireLock(ctx, store.lockPath)
	if err != nil {
		return Job{}, err
	}
	state, err := readState(store.path)
	changed := false
	if err == nil {
		state, changed, err = store.recoverStale(state)
	}
	if err == nil && changed {
		err = store.writeState(store.path, state)
	}
	if err != nil {
		return Job{}, store.closeWithError(lock, err)
	}
	for _, job := range state.Jobs {
		if job.ID == id {
			return job, store.closeWithError(lock, nil)
		}
	}
	return Job{}, store.closeWithError(lock, ErrNotFound)
}

func (store *Store) Retry(ctx context.Context, id string) error {
	return store.update(ctx, id, func(job *Job, now time.Time) error {
		switch job.Status {
		case StatusQueued, StatusFailed, StatusCanceled:
			job.Status = StatusQueued
			job.Attempts++
			job.LeaseID = ""
			job.Error = ""
			job.CompletedAt = nil
			job.UpdatedAt = now
			return nil
		case StatusRunning, StatusCompleted:
			return ErrInvalidTransition
		default:
			return ErrInvalidState
		}
	})
}

func (store *Store) Cancel(ctx context.Context, id string) error {
	return store.update(ctx, id, func(job *Job, now time.Time) error {
		switch job.Status {
		case StatusQueued, StatusRunning, StatusFailed:
			job.Status = StatusCanceled
			job.LeaseID = ""
			job.StartedAt = nil
			job.UpdatedAt = now
			return nil
		case StatusCanceled, StatusCompleted:
			return ErrInvalidTransition
		default:
			return ErrInvalidState
		}
	})
}

func (store *Store) update(ctx context.Context, id string, change func(*Job, time.Time) error) error {
	lock, err := acquireLock(ctx, store.lockPath)
	if err != nil {
		return err
	}
	state, err := readState(store.path)
	if err == nil {
		state, _, err = store.recoverStale(state)
	}
	if err != nil {
		return store.closeWithError(lock, err)
	}
	found := false
	for index := range state.Jobs {
		if state.Jobs[index].ID == id {
			if err := change(&state.Jobs[index], store.now().UTC()); err != nil {
				return store.closeWithError(lock, err)
			}
			found = true
			break
		}
	}
	if !found {
		return store.closeWithError(lock, ErrNotFound)
	}
	if err := store.writeState(store.path, state); err != nil {
		return store.closeWithError(lock, err)
	}
	return store.closeWithError(lock, nil)
}

func (store *Store) closeWithError(lock *processLock, operationErr error) error {
	closeErr := lock.Close()
	if operationErr != nil {
		return operationErr
	}
	return closeErr
}

func (store *Store) recoverStale(state persistedState) (persistedState, bool, error) {
	now := store.now().UTC()
	changed := false
	for index := range state.Jobs {
		job := &state.Jobs[index]
		if job.Status == StatusRunning && !job.UpdatedAt.IsZero() && now.Sub(job.UpdatedAt) > store.staleAfter {
			job.Status = StatusQueued
			job.Error = "recovered stale running job"
			job.StartedAt = nil
			job.LeaseID = ""
			job.UpdatedAt = now
			changed = true
		}
	}
	return state, changed, nil
}

func newID(now time.Time) string {
	random := make([]byte, 5)
	if _, err := rand.Read(random); err != nil {
		random = []byte(fmt.Sprintf("%d", now.UnixNano()))
	}
	return "job-" + now.UTC().Format("20060102t150405") + "-" + hex.EncodeToString(random)
}
