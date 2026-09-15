package queue

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidJSON       = errors.New("queue: invalid JSON")
	ErrInvalidName       = errors.New("queue: invalid job name")
	ErrInvalidOutputPath = errors.New("queue: invalid output path")
	ErrLocked            = errors.New("queue: store is locked")
	ErrNotFound          = errors.New("queue: job not found")
	ErrInvalidTransition = errors.New("queue: invalid job status transition")
	ErrInvalidState      = errors.New("queue: invalid queue state")
	ErrInvalidJobSchema  = errors.New("queue: invalid job schema")
)

type JobSchemaError struct {
	JobID  string
	Field  string
	Reason string
}

func (err *JobSchemaError) Error() string {
	return fmt.Sprintf("queue: invalid job %q field %q: %s", err.JobID, err.Field, err.Reason)
}

func (err *JobSchemaError) Is(target error) bool { return target == ErrInvalidJobSchema }
