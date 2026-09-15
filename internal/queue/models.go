package queue

import (
	"time"
	"video-downloader/internal/downloader"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

type Job struct {
	DownloadOptions downloader.Overrides `json:"download_options,omitempty"`
	ID              string               `json:"id"`
	URL             string               `json:"url"`
	Name            string               `json:"name"`
	OutputPath      string               `json:"output_path"`
	Status          Status               `json:"status"`
	Attempts        int                  `json:"attempts"`
	LeaseID         string               `json:"lease_id,omitempty"`
	BytesDownloaded int64                `json:"bytes_downloaded"`
	BytesTotal      int64                `json:"bytes_total"`
	SegmentIndex    int                  `json:"segment_index"`
	SegmentTotal    int                  `json:"segment_total"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`
	StartedAt       *time.Time           `json:"started_at,omitempty"`
	CompletedAt     *time.Time           `json:"completed_at,omitempty"`
	Error           string               `json:"error,omitempty"`
}

type AddInput struct {
	DownloadOptions downloader.Overrides
	URL             string
	Name            string
	OutputPath      string
}

type Options struct {
	StaleAfter time.Duration
	Now        func() time.Time
}

type persistedState struct {
	Version int   `json:"version"`
	Jobs    []Job `json:"jobs"`
}
