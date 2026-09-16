package queue

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

var ErrSupervisorRunning = errors.New("un superviseur est déjà actif pour cette file ; utilisez videodl watch pour le suivi")

// The persistent inode must never be removed while a process may hold its lock.
func (store *Store) LockSupervisor() (func(), error) {
	fd, err := syscall.Open(store.path+".worker.lock", syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, fmt.Errorf("verrou du superviseur: %w", err)
	}
	file := os.NewFile(uintptr(fd), store.path+".worker.lock")
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrSupervisorRunning
		}
		return nil, err
	}
	return func() { syscall.Flock(fd, syscall.LOCK_UN); file.Close() }, nil
}
