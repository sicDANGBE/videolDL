package queue

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func readState(path string) (persistedState, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return persistedState{Version: 1, Jobs: []Job{}}, nil
	}
	if err != nil {
		return persistedState{}, fmt.Errorf("read queue state: %w", err)
	}
	var state persistedState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil || state.Version != 1 {
		return persistedState{}, ErrInvalidJSON
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return persistedState{}, ErrInvalidJSON
	}
	if err := validateState(state); err != nil {
		return persistedState{}, err
	}
	return state, nil
}

func writeState(path string, state persistedState) error {
	return writeStateWithRename(path, state, os.Rename)
}

func writeStateWithRename(path string, state persistedState, rename func(string, string) error) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode queue state: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".queue-*.tmp")
	if err != nil {
		return fmt.Errorf("create queue temporary state: %w", err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set queue state permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write queue state: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync queue state: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close queue state: %w", err)
	}
	if err := rename(tempPath, path); err != nil {
		return fmt.Errorf("replace queue state: %w", err)
	}
	removeTemp = false
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open queue directory: %w", err)
	}
	defer func() { _ = directory.Close() }()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync queue directory: %w", err)
	}
	return nil
}

func Snapshot(path string) ([]Job, error) { state, err := readState(path); return state.Jobs, err }
