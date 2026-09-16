package downloader

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

var ErrInsufficientSpace = errors.New("espace disque insuffisant")

const unknownReservation int64 = 64 << 20

type diskInfo struct {
	device uint64
	free   int64
}

func inspectDisk(path string) (diskInfo, error) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return diskInfo{}, err
	}
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		return diskInfo{}, err
	}
	return diskInfo{device: uint64(stat.Dev), free: int64(fs.Bavail) * int64(fs.Bsize)}, nil
}

type diskBudget struct {
	mu      sync.Mutex
	inspect func(string) (diskInfo, error)
	active  map[*diskReservation]bool
}
type diskReservation struct {
	budget           *diskBudget
	directory        string
	device           uint64
	remaining, floor int64
	known            bool
	checked          time.Time
	sinceCheck       int64
}

func newDiskBudget() *diskBudget {
	return &diskBudget{inspect: inspectDisk, active: map[*diskReservation]bool{}}
}
func (b *diskBudget) reserve(output string, floor int64) (*diskReservation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	info, err := b.inspect(filepath.Dir(output))
	if err != nil {
		return nil, fmt.Errorf("vérifier l'espace disque: %w", err)
	}
	r := &diskReservation{budget: b, directory: filepath.Dir(output), device: info.device, floor: floor, remaining: unknownReservation}
	b.active[r] = true
	if err := r.checkLocked(); err != nil {
		delete(b.active, r)
		return nil, err
	}
	return r, nil
}
func (r *diskReservation) checkLocked() error {
	info, err := r.budget.inspect(r.directory)
	if err != nil {
		return fmt.Errorf("vérifier l'espace disque: %w", err)
	}
	if info.device != r.device {
		return fmt.Errorf("le système de fichiers de destination a changé")
	}
	var required, floor int64
	for item := range r.budget.active {
		if item.device == r.device {
			if item.remaining > math.MaxInt64-required {
				required = math.MaxInt64
			} else {
				required += item.remaining
			}
			floor = max(floor, item.floor)
		}
	}
	if info.free < floor || info.free-floor < required {
		return fmt.Errorf("%w : %.2f Gio disponibles, %.2f Gio réservés aux transferts et %.2f Gio de marge", ErrInsufficientSpace, float64(info.free)/(1<<30), float64(required)/(1<<30), float64(floor)/(1<<30))
	}
	r.checked = time.Now()
	r.sinceCheck = 0
	return nil
}
func (r *diskReservation) size(remaining int64) error {
	if r == nil {
		return nil
	}
	r.budget.mu.Lock()
	defer r.budget.mu.Unlock()
	old, known := r.remaining, r.known
	r.remaining = max(remaining, 0)
	r.known = remaining >= 0
	if !r.known {
		r.remaining = unknownReservation
	}
	if err := r.checkLocked(); err != nil {
		r.remaining, r.known = old, known
		return err
	}
	return nil
}
func (r *diskReservation) release() {
	if r == nil {
		return
	}
	r.budget.mu.Lock()
	delete(r.budget.active, r)
	r.budget.mu.Unlock()
}
func (r *diskReservation) writer(out io.Writer) io.Writer {
	if r == nil {
		return out
	}
	return diskWriter{out: out, reservation: r}
}

type diskWriter struct {
	out         io.Writer
	reservation *diskReservation
}

func (w diskWriter) Write(data []byte) (int, error) {
	r := w.reservation
	r.budget.mu.Lock()
	defer r.budget.mu.Unlock()
	// Native writes are short buffered writes. Check at least each MiB or 500ms,
	// and immediately when extending an unknown-length reservation.
	extended := false
	if !r.known && r.remaining < int64(len(data)) {
		r.remaining = max(unknownReservation, int64(len(data)))
		extended = true
	}
	if extended || r.sinceCheck >= 1<<20 || time.Since(r.checked) >= 500*time.Millisecond {
		if err := r.checkLocked(); err != nil {
			return 0, err
		}
	}
	n, err := w.out.Write(data)
	r.remaining = max(0, r.remaining-int64(n))
	r.sinceCheck += int64(n)
	return n, err
}

// FFmpeg writes outside Go. Account for growth and check available disk every
// 250ms; this is a best-effort guard, not a filesystem quota.
func (r *diskReservation) external(path string, previous *int64) error {
	if r == nil {
		return nil
	}
	r.budget.mu.Lock()
	defer r.budget.mu.Unlock()
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	growth := max(0, info.Size()-*previous)
	*previous = info.Size()
	r.remaining = max(0, r.remaining-growth)
	if r.remaining < unknownReservation/2 {
		r.remaining = unknownReservation
	}
	return r.checkLocked()
}
