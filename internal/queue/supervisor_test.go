package queue

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSupervisorLockIsExclusiveAndReusable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	a, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	release, err := a.LockSupervisor()
	if err != nil {
		t.Fatal(err)
	}
	if second, err := b.LockSupervisor(); !errors.Is(err, ErrSupervisorRunning) {
		if second != nil {
			second()
		}
		release()
		t.Fatalf("second supervisor: %v", err)
	}
	before, err := os.Stat(path + ".worker.lock")
	if err != nil {
		release()
		t.Fatal(err)
	}
	release()
	second, err := b.LockSupervisor()
	if err != nil {
		t.Fatal(err)
	}
	defer second()
	after, err := os.Stat(path + ".worker.lock")
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("lock inode replaced")
	}
}
