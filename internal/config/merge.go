package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"
)

type fileConfig struct {
	MinFreeSpace    *string `json:"min_free_space"`
	IdleTimeout     *string `json:"idle_timeout"`
	MaxHeight       *int    `json:"max_height"`
	Resume          *bool   `json:"resume"`
	Retries         *int    `json:"retries"`
	StatePath       *string `json:"state_path"`
	LogPath         *string `json:"log_path"`
	Destination     *string `json:"destination"`
	Concurrency     *int    `json:"concurrency"`
	Timeout         *string `json:"timeout"`
	FFmpeg          *bool   `json:"ffmpeg"`
	FFmpegPath      *string `json:"ffmpeg_path"`
	WebhookURL      *string `json:"webhook_url"`
	Editor          *string `json:"editor"`
	AutoStartWorker *bool   `json:"auto_start_worker"`
	DaemonPIDPath   *string `json:"daemon_pid_path"`
	DaemonLogPath   *string `json:"daemon_log_path"`
	NotifyCommand   *string `json:"notify_command"`
}

func ApplyOverrides(config *Config, overrides Overrides) error {
	if overrides.IdleTimeout != nil {
		config.IdleTimeout = *overrides.IdleTimeout
	}
	if overrides.MaxHeight != nil {
		config.MaxHeight = *overrides.MaxHeight
	}
	if overrides.Resume != nil {
		config.Resume = *overrides.Resume
	}
	if overrides.Retries != nil {
		config.Retries = *overrides.Retries
	}
	if overrides.ConfigPath != "" {
		config.ConfigPath = overrides.ConfigPath
	}
	if overrides.StatePath != nil {
		config.StatePath = *overrides.StatePath
	}
	if overrides.LogPath != nil {
		config.LogPath = *overrides.LogPath
	}
	if overrides.Destination != nil {
		config.Destination = *overrides.Destination
	}
	if overrides.Concurrency != nil {
		config.Concurrency = *overrides.Concurrency
	}
	if overrides.Timeout != nil {
		config.Timeout = *overrides.Timeout
	}
	if overrides.FFmpeg != nil {
		config.FFmpeg = *overrides.FFmpeg
	}
	if overrides.FFmpegPath != nil {
		config.FFmpegPath = *overrides.FFmpegPath
	}
	if overrides.WebhookURL != nil {
		config.WebhookURL = *overrides.WebhookURL
	}
	if overrides.Editor != nil {
		config.Editor = *overrides.Editor
	}
	if overrides.AutoStartWorker != nil {
		config.AutoStartWorker = *overrides.AutoStartWorker
	}
	if overrides.DaemonPIDPath != nil {
		config.DaemonPIDPath = *overrides.DaemonPIDPath
	}
	if overrides.DaemonLogPath != nil {
		config.DaemonLogPath = *overrides.DaemonLogPath
	}
	if overrides.NotifyCommand != nil {
		config.NotifyCommand = *overrides.NotifyCommand
	}
	return validate(config)
}

func loadFile(config *Config, path string, explicit bool) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && !explicit {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read config file %q: %w", path, err)
	}
	var decoded fileConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return fmt.Errorf("%w: config file could not be decoded", ErrInvalidJSON)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: config file contains multiple JSON values", ErrInvalidJSON)
	}
	if decoded.MinFreeSpace != nil {
		value, err := ParseSpace(*decoded.MinFreeSpace)
		if err != nil {
			return err
		}
		config.MinFreeSpace = value
	}
	if decoded.Retries != nil {
		config.Retries = *decoded.Retries
	}
	if decoded.Resume != nil {
		config.Resume = *decoded.Resume
	}
	if decoded.MaxHeight != nil {
		config.MaxHeight = *decoded.MaxHeight
	}
	if decoded.IdleTimeout != nil {
		value, err := time.ParseDuration(*decoded.IdleTimeout)
		if err != nil {
			return &ValidationError{Field: "idle_timeout", Rule: "must be a duration"}
		}
		config.IdleTimeout = value
	}
	if decoded.StatePath != nil {
		config.StatePath = *decoded.StatePath
	}
	if decoded.LogPath != nil {
		config.LogPath = *decoded.LogPath
	}
	if decoded.Destination != nil {
		config.Destination = *decoded.Destination
	}
	if decoded.Concurrency != nil {
		config.Concurrency = *decoded.Concurrency
	}
	if decoded.Timeout != nil {
		timeout, err := time.ParseDuration(*decoded.Timeout)
		if err != nil {
			return &ValidationError{Field: "timeout", Rule: "must be a valid duration"}
		}
		config.Timeout = timeout
	}
	if decoded.FFmpeg != nil {
		config.FFmpeg = *decoded.FFmpeg
	}
	if decoded.FFmpegPath != nil {
		config.FFmpegPath = *decoded.FFmpegPath
	}
	if decoded.WebhookURL != nil {
		config.WebhookURL = *decoded.WebhookURL
	}
	if decoded.Editor != nil {
		config.Editor = *decoded.Editor
	}
	if decoded.AutoStartWorker != nil {
		config.AutoStartWorker = *decoded.AutoStartWorker
	}
	if decoded.DaemonPIDPath != nil {
		config.DaemonPIDPath = *decoded.DaemonPIDPath
	}
	if decoded.DaemonLogPath != nil {
		config.DaemonLogPath = *decoded.DaemonLogPath
	}
	if decoded.NotifyCommand != nil {
		config.NotifyCommand = *decoded.NotifyCommand
	}
	return validate(config)
}

func ApplyEnvironment(config *Config, env []string) error {
	values := map[string]string{}
	for _, item := range env {
		for _, key := range []string{"VIDEODL_MIN_FREE_SPACE", "VIDEODL_RESUME", "VIDEODL_MAX_HEIGHT", "VIDEODL_IDLE_TIMEOUT", "VIDEODL_RETRIES", "VIDEODL_CONFIG", "VIDEODL_STATE_PATH", "VIDEODL_LOG_PATH", "VIDEODL_DESTINATION", "VIDEODL_CONCURRENCY", "VIDEODL_TIMEOUT", "VIDEODL_FFMPEG", "VIDEODL_FFMPEG_PATH", "VIDEODL_WEBHOOK_URL", "VIDEODL_EDITOR", "VIDEODL_AUTO_START_WORKER", "VIDEODL_DAEMON_PID_PATH", "VIDEODL_DAEMON_LOG_PATH", "VIDEODL_NOTIFY_COMMAND"} {
			if len(item) > len(key)+1 && item[:len(key)] == key && item[len(key)] == '=' {
				values[key] = item[len(key)+1:]
			}
		}
	}
	if value, ok := values["VIDEODL_MIN_FREE_SPACE"]; ok {
		parsed, err := ParseSpace(value)
		if err != nil {
			return err
		}
		config.MinFreeSpace = parsed
	}
	if value, ok := values["VIDEODL_RETRIES"]; ok {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return &ValidationError{Field: "retries", Rule: "invalid value"}
		}
		config.Retries = parsed
	}
	if value, ok := values["VIDEODL_RESUME"]; ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return &ValidationError{Field: "resume", Rule: "invalid value"}
		}
		config.Resume = parsed
	}
	if value, ok := values["VIDEODL_MAX_HEIGHT"]; ok {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return &ValidationError{Field: "max_height", Rule: "invalid value"}
		}
		config.MaxHeight = parsed
	}
	if value, ok := values["VIDEODL_IDLE_TIMEOUT"]; ok {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return &ValidationError{Field: "idle_timeout", Rule: "invalid value"}
		}
		config.IdleTimeout = parsed
	}
	if value, ok := values["VIDEODL_STATE_PATH"]; ok {
		config.StatePath = value
	}
	if value, ok := values["VIDEODL_LOG_PATH"]; ok {
		config.LogPath = value
	}
	if value, ok := values["VIDEODL_DESTINATION"]; ok {
		config.Destination = value
	}
	if value, ok := values["VIDEODL_CONCURRENCY"]; ok {
		concurrency, err := strconv.Atoi(value)
		if err != nil {
			return &ValidationError{Field: "concurrency", Rule: "must be an integer from 1 through 8"}
		}
		config.Concurrency = concurrency
	}
	if value, ok := values["VIDEODL_TIMEOUT"]; ok {
		timeout, err := time.ParseDuration(value)
		if err != nil {
			return &ValidationError{Field: "timeout", Rule: "must be a valid duration"}
		}
		config.Timeout = timeout
	}
	if value, ok := values["VIDEODL_FFMPEG"]; ok {
		ffmpeg, err := strconv.ParseBool(value)
		if err != nil {
			return &ValidationError{Field: "ffmpeg", Rule: "must be a boolean"}
		}
		config.FFmpeg = ffmpeg
	}
	if value, ok := values["VIDEODL_FFMPEG_PATH"]; ok {
		config.FFmpegPath = value
	}
	if value, ok := values["VIDEODL_WEBHOOK_URL"]; ok {
		config.WebhookURL = value
	}
	if value, ok := values["VIDEODL_EDITOR"]; ok {
		config.Editor = value
	}
	if value, ok := values["VIDEODL_AUTO_START_WORKER"]; ok {
		autoStartWorker, err := strconv.ParseBool(value)
		if err != nil {
			return &ValidationError{Field: "auto_start_worker", Rule: "must be a boolean"}
		}
		config.AutoStartWorker = autoStartWorker
	}
	if value, ok := values["VIDEODL_DAEMON_PID_PATH"]; ok {
		config.DaemonPIDPath = value
	}
	if value, ok := values["VIDEODL_DAEMON_LOG_PATH"]; ok {
		config.DaemonLogPath = value
	}
	if value, ok := values["VIDEODL_NOTIFY_COMMAND"]; ok {
		config.NotifyCommand = value
	}
	return validate(config)
}

func validate(config *Config) error {
	if config.MinFreeSpace < 0 {
		return &ValidationError{Field: "min_free_space", Rule: "must be nonnegative"}
	}
	if config.Retries < 0 || config.Retries > 10 {
		return &ValidationError{Field: "retries", Rule: "must be between 0 and 10"}
	}
	if config.MaxHeight < 0 || config.MaxHeight > 8640 {
		return &ValidationError{Field: "max_height", Rule: "must be between 0 and 8640 (0 = best)"}
	}
	if config.IdleTimeout <= 0 {
		return &ValidationError{Field: "idle_timeout", Rule: "must be positive"}
	}
	if config.Concurrency < 1 || config.Concurrency > 8 {
		return &ValidationError{Field: "concurrency", Rule: "must be an integer from 1 through 8"}
	}
	if config.Timeout <= 0 {
		return &ValidationError{Field: "timeout", Rule: "must be positive"}
	}
	return nil
}
