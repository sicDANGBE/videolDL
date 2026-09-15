package queue

import (
	"context"
	"fmt"
	"os"
	"time"
)

type processLock struct {
	file *os.File
	path string
}

func acquireLock(ctx context.Context, path string) (*processLock, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("acquire queue lock: %w", err)
	}
	var file *os.File
	var err error
	deadline := time.Now().Add(time.Second)
	for {
		file, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if !os.IsExist(err) || time.Now().After(deadline) {
			break
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	if err != nil {
		if os.IsExist(err) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("create queue lock: %w", err)
	}
	if _, err := fmt.Fprintf(file, "%d\n", os.Getpid()); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("write queue lock: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("sync queue lock: %w", err)
	}
	return &processLock{file: file, path: path}, nil
}

func (lock *processLock) Close() error {
	closeErr := lock.file.Close()
	removeErr := os.Remove(lock.path)
	if closeErr != nil {
		return fmt.Errorf("close queue lock: %w", closeErr)
	}
	if removeErr != nil && !os.IsNotExist(removeErr) {
		return fmt.Errorf("remove queue lock: %w", removeErr)
	}
	return nil
}
