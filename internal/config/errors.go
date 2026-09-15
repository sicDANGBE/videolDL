package config

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidJSON        = errors.New("config: invalid JSON")
	ErrInvalidConcurrency = errors.New("config: invalid concurrency")
	ErrInvalidPath        = errors.New("config: invalid path")
)

type ValidationError struct {
	Field string
	Rule  string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("config: %s violates %s", e.Field, e.Rule)
}

func (e *ValidationError) Is(target error) bool {
	switch {
	case target == ErrInvalidConcurrency:
		return e.Field == "concurrency"
	case target == ErrInvalidPath:
		return e.Field == "destination" || e.Field == "state_path" || e.Field == "log_path" || e.Field == "daemon_pid_path" || e.Field == "daemon_log_path"
	default:
		return false
	}
}
