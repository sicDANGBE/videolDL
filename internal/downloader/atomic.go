package downloader

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func writeAtomic(outputPath string, write func(io.Writer) error) (err error) {
	directory := filepath.Dir(outputPath)
	base := filepath.Base(outputPath)
	extension := filepath.Ext(base)
	temporary, err := os.CreateTemp(directory, "."+base+".*.part"+extension)
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	temporaryPath := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			if closeErr := temporary.Close(); closeErr != nil && err == nil {
				err = fmt.Errorf("close temporary output: %w", closeErr)
			}
		}
		if err != nil {
			if removeErr := os.Remove(temporaryPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, fmt.Errorf("remove temporary output: %w", removeErr))
			}
		}
	}()

	if err := write(temporary); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary output: %w", err)
	}
	closed = true
	if err := publishOutput(temporaryPath, outputPath); err != nil {
		return fmt.Errorf("rename output: %w", err)
	}
	return nil
}

// Both paths are on the same filesystem. Link never replaces a destination.
func publishOutput(temporaryPath, outputPath string) error {
	if err := os.Link(temporaryPath, outputPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrOutputExists
		}
		return fmt.Errorf("publish output: %w", err)
	}
	return os.Remove(temporaryPath)
}
