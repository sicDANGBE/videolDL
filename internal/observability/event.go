package observability

import (
	"errors"
	"net/url"
	"strings"
	"time"
)

type State string

const (
	StateStarted   State = "started"
	StateProgress  State = "progress"
	StateSucceeded State = "succeeded"
	StateFailed    State = "failed"
	StateCanceled  State = "canceled"
)

var ErrInvalidEvent = errors.New("invalid observability event")

type ErrorInfo struct {
	Message string `json:"message"`
}

type Event struct {
	JobID           string     `json:"job_id"`
	URL             string     `json:"url"`
	Name            string     `json:"name"`
	OutputPath      string     `json:"output_path"`
	State           State      `json:"state"`
	StartedAt       time.Time  `json:"started_at"`
	FinishedAt      time.Time  `json:"finished_at"`
	BytesDownloaded int64      `json:"bytes_downloaded"`
	BytesTotal      int64      `json:"bytes_total"`
	Error           *ErrorInfo `json:"error,omitempty"`
}

func (event Event) valid() error {
	if strings.TrimSpace(event.JobID) == "" || strings.TrimSpace(event.Name) == "" || strings.TrimSpace(event.URL) == "" || strings.TrimSpace(event.OutputPath) == "" {
		return ErrInvalidEvent
	}
	if event.BytesDownloaded < 0 || event.BytesTotal < 0 {
		return ErrInvalidEvent
	}
	switch event.State {
	case StateStarted, StateProgress, StateSucceeded, StateFailed, StateCanceled:
	default:
		return ErrInvalidEvent
	}
	parsed, err := url.Parse(event.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ErrInvalidEvent
	}
	if event.State == StateFailed && event.Error == nil {
		return ErrInvalidEvent
	}
	if event.State != StateFailed && event.Error != nil {
		return ErrInvalidEvent
	}
	return nil
}

func (event Event) safe() Event {
	safe := event
	parsed, err := url.Parse(event.URL)
	if err == nil {
		parsed.User = nil
		parsed.RawQuery = ""
		parsed.Fragment = ""
		safe.URL = parsed.String()
	}
	if event.Error != nil {
		safe.Error = &ErrorInfo{Message: redactMessage(event.Error.Message)}
	}
	return safe
}

func redactMessage(message string) string {
	var redacted strings.Builder
	position := 0
	for position < len(message) {
		start := nextURLStart(message, position)
		if start < 0 {
			redacted.WriteString(message[position:])
			break
		}
		redacted.WriteString(message[position:start])
		end := start
		for end < len(message) && !strings.ContainsRune(" \t\r\n,;)", rune(message[end])) {
			end++
		}
		candidate := message[start:end]
		parsed, err := url.Parse(candidate)
		if err == nil && parsed.Host != "" {
			parsed.User = nil
			parsed.RawQuery = ""
			parsed.Fragment = ""
			redacted.WriteString(parsed.String())
		} else {
			redacted.WriteString(candidate)
		}
		position = end
	}
	return redacted.String()
}

func nextURLStart(message string, from int) int {
	httpStart := strings.Index(message[from:], "http://")
	httpsStart := strings.Index(message[from:], "https://")
	if httpStart < 0 {
		if httpsStart < 0 {
			return -1
		}
		return from + httpsStart
	}
	if httpsStart < 0 {
		return from + httpStart
	}
	if httpStart < httpsStart {
		return from + httpStart
	}
	return from + httpsStart
}
