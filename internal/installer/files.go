package installer

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

type stagedFile struct {
	target, prepared, backup string
	applied                  bool
}

func copyToTemp(directory string, input io.Reader, mode os.FileMode) (string, error) {
	file, err := os.CreateTemp(directory, ".videodl-update-*")
	if err != nil {
		return "", err
	}
	name := file.Name()
	ok := false
	defer func() {
		file.Close()
		if !ok {
			os.Remove(name)
		}
	}()
	if _, err := io.Copy(file, input); err != nil {
		return "", err
	}
	if err := file.Chmod(mode); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}
func stage(target string, input io.Reader, mode os.FileMode) (stagedFile, error) {
	staged := stagedFile{target: target}
	directory := filepath.Dir(target)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return staged, err
	}
	info, err := os.Lstat(target)
	if err == nil {
		if !info.Mode().IsRegular() {
			return staged, fmt.Errorf("la cible n’est pas un fichier ordinaire : %s", target)
		}
		old, err := os.Open(target)
		if err != nil {
			return staged, err
		}
		staged.backup, err = copyToTemp(directory, old, info.Mode().Perm())
		old.Close()
		if err != nil {
			return staged, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return staged, err
	}
	staged.prepared, err = copyToTemp(directory, input, mode)
	if err != nil {
		staged.cleanup()
	}
	return staged, err
}
func (s stagedFile) cleanup() {
	if s.prepared != "" {
		os.Remove(s.prepared)
	}
	if s.backup != "" {
		os.Remove(s.backup)
	}
}
func (s *stagedFile) restore() error {
	if !s.applied {
		return nil
	}
	if s.backup != "" {
		return os.Rename(s.backup, s.target)
	}
	return os.Remove(s.target)
}
func lockInstall(prefix string) (func(), error) {
	if err := os.MkdirAll(prefix, 0755); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(filepath.Join(prefix, ".videodl-install.lock"), syscall.O_RDWR|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "install lock")
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("une autre installation est en cours : %w", err)
	}
	return func() { syscall.Flock(fd, syscall.LOCK_UN); file.Close() }, nil
}
