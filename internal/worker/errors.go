package worker

import "errors"

var (
	ErrInvalidDependency  = errors.New("worker dependency is nil")
	ErrInvalidConcurrency = errors.New("worker concurrency must be between 1 and 8")
)
