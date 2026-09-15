package queue

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"video-downloader/internal/downloader"
)

func validateState(state persistedState) error {
	if state.Version != 1 {
		return ErrInvalidState
	}
	if state.Jobs == nil {
		return &JobSchemaError{Field: "jobs", Reason: "required array is missing"}
	}
	for _, job := range state.Jobs {
		if err := validatePersistedJob(job); err != nil {
			return err
		}
	}
	return nil
}

func validatePersistedJob(job Job) error {
	if err := validateDownloadOptions(job.DownloadOptions); err != nil {
		return err
	}
	if job.ID == "" {
		return &JobSchemaError{Field: "id", Reason: "required"}
	}
	if strings.TrimSpace(job.URL) == "" {
		return &JobSchemaError{JobID: job.ID, Field: "url", Reason: "required"}
	}
	parsed, err := url.ParseRequestURI(job.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return &JobSchemaError{JobID: job.ID, Field: "url", Reason: "invalid URL"}
	}
	if err := validateName(job.Name); err != nil {
		return &JobSchemaError{JobID: job.ID, Field: "name", Reason: "invalid name"}
	}
	if err := validateOutputPath(job.OutputPath); err != nil {
		return &JobSchemaError{JobID: job.ID, Field: "output_path", Reason: "invalid path"}
	}
	if err := validateStatus(job.Status); err != nil {
		return &JobSchemaError{JobID: job.ID, Field: "status", Reason: err.Error()}
	}
	if job.Attempts < 0 || job.BytesDownloaded < 0 || job.BytesTotal < 0 {
		return &JobSchemaError{JobID: job.ID, Field: "counters", Reason: "must not be negative"}
	}
	if job.CreatedAt.IsZero() || job.UpdatedAt.IsZero() {
		return &JobSchemaError{JobID: job.ID, Field: "timestamps", Reason: "created_at and updated_at are required"}
	}
	return nil
}

func validateStatus(status Status) error {
	switch status {
	case StatusQueued, StatusRunning, StatusCompleted, StatusFailed, StatusCanceled:
		return nil
	default:
		return fmt.Errorf("unknown status %q", status)
	}
}

func validateInput(input AddInput) error {
	if err := validateDownloadOptions(input.DownloadOptions); err != nil {
		return err
	}
	if strings.TrimSpace(input.URL) == "" {
		return fmt.Errorf("url is empty")
	}
	parsed, err := url.ParseRequestURI(input.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		if err != nil {
			return fmt.Errorf("invalid URL: %w", err)
		}
		return fmt.Errorf("invalid URL")
	}
	if err := validateName(input.Name); err != nil {
		return err
	}
	return validateOutputPath(input.OutputPath)
}

func validateName(name string) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsRune(name, 0) {
		return ErrInvalidName
	}
	if strings.ContainsAny(name, `/\\`) {
		return ErrInvalidName
	}
	return nil
}

func validateOutputPath(path string) error {
	if path == "" || strings.ContainsRune(path, 0) || !filepath.IsAbs(path) {
		return ErrInvalidOutputPath
	}
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		if part == ".." {
			return ErrInvalidOutputPath
		}
	}
	return nil
}

func validateDownloadOptions(o downloader.Overrides) error {
	if o.Retries != nil && (*o.Retries < 0 || *o.Retries > 10) {
		return fmt.Errorf("invalid job retries")
	}
	if o.MaxHeight != nil && (*o.MaxHeight < 0 || *o.MaxHeight > 8640) {
		return fmt.Errorf("invalid job max height")
	}
	if o.IdleTimeout != nil && *o.IdleTimeout <= 0 {
		return fmt.Errorf("invalid job idle timeout")
	}
	return nil
}
