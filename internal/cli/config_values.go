package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"video-downloader/internal/config"
)

func fromConfig(loaded config.Config) persistedConfig {
	return persistedConfig{Retries: loaded.Retries, Resume: loaded.Resume, MaxHeight: loaded.MaxHeight, IdleTimeout: loaded.IdleTimeout.String(), StatePath: loaded.StatePath, LogPath: loaded.LogPath, Destination: loaded.Destination, Concurrency: loaded.Concurrency, Timeout: loaded.Timeout.String(), FFmpeg: loaded.FFmpeg, FFmpegPath: loaded.FFmpegPath, WebhookURL: loaded.WebhookURL, Editor: loaded.Editor, AutoStartWorker: loaded.AutoStartWorker, DaemonPIDPath: loaded.DaemonPIDPath, DaemonLogPath: loaded.DaemonLogPath, NotifyCommand: loaded.NotifyCommand}
}

func formatConfig(stored persistedConfig) string {
	var builder strings.Builder
	for _, key := range completionConfigKeys {
		fmt.Fprintf(&builder, "%s: %s\n", key, mustConfigValue(stored, key))
	}
	return builder.String()
}

func configValue(stored persistedConfig, key string) (string, error) {
	switch key {
	case "idle_timeout":
		return stored.IdleTimeout, nil
	case "max_height":
		return strconv.Itoa(stored.MaxHeight), nil
	case "resume":
		return strconv.FormatBool(stored.Resume), nil
	case "retries":
		return strconv.Itoa(stored.Retries), nil
	case "state_path":
		return stored.StatePath, nil
	case "log_path":
		return stored.LogPath, nil
	case "destination":
		return stored.Destination, nil
	case "concurrency":
		return strconv.Itoa(stored.Concurrency), nil
	case "timeout":
		return stored.Timeout, nil
	case "ffmpeg":
		return strconv.FormatBool(stored.FFmpeg), nil
	case "ffmpeg_path":
		return stored.FFmpegPath, nil
	case "webhook_url":
		return stored.WebhookURL, nil
	case "editor":
		return stored.Editor, nil
	case "auto_start_worker":
		return strconv.FormatBool(stored.AutoStartWorker), nil
	case "daemon_pid_path":
		return stored.DaemonPIDPath, nil
	case "daemon_log_path":
		return stored.DaemonLogPath, nil
	case "notify_command":
		return stored.NotifyCommand, nil
	default:
		return "", fmt.Errorf("unknown config key %q", key)
	}
}

func mustConfigValue(stored persistedConfig, key string) string {
	value, err := configValue(stored, key)
	if err != nil {
		return ""
	}
	return value
}

func setConfigValue(stored *persistedConfig, key, value, workingDir string) error {
	switch key {
	case "idle_timeout":
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return &config.ValidationError{Field: key, Rule: "invalid value"}
		}
		stored.IdleTimeout = parsed.String()
	case "max_height":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return &config.ValidationError{Field: key, Rule: "invalid value"}
		}
		stored.MaxHeight = parsed
	case "resume":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return &config.ValidationError{Field: key, Rule: "invalid value"}
		}
		stored.Resume = parsed
	case "retries":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return &config.ValidationError{Field: key, Rule: "invalid value"}
		}
		stored.Retries = parsed
	case "state_path":
		stored.StatePath = value
	case "log_path":
		stored.LogPath = value
	case "destination":
		resolved, err := config.ResolveDestination(value, workingDir)
		if err != nil {
			return err
		}
		stored.Destination = resolved
	case "concurrency":
		concurrency, err := strconv.Atoi(value)
		if err != nil {
			return &config.ValidationError{Field: key, Rule: "must be an integer from 1 through 8"}
		}
		stored.Concurrency = concurrency
	case "timeout":
		timeout, err := time.ParseDuration(value)
		if err != nil {
			return &config.ValidationError{Field: key, Rule: "must be a valid duration"}
		}
		stored.Timeout = timeout.String()
	case "ffmpeg":
		ffmpeg, err := strconv.ParseBool(value)
		if err != nil {
			return &config.ValidationError{Field: key, Rule: "must be a boolean"}
		}
		stored.FFmpeg = ffmpeg
	case "ffmpeg_path":
		stored.FFmpegPath = value
	case "webhook_url":
		stored.WebhookURL = value
	case "editor":
		stored.Editor = value
	case "auto_start_worker":
		autoStartWorker, err := strconv.ParseBool(value)
		if err != nil {
			return &config.ValidationError{Field: key, Rule: "must be a boolean"}
		}
		stored.AutoStartWorker = autoStartWorker
	case "daemon_pid_path":
		stored.DaemonPIDPath = value
	case "daemon_log_path":
		stored.DaemonLogPath = value
	case "notify_command":
		stored.NotifyCommand = value
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return nil
}

func selectEditor(flagEditor, configEditor string, env []string) (string, error) {
	for _, candidate := range []string{flagEditor, envValue(env, "VISUAL"), envValue(env, "EDITOR"), configEditor, "nano", "vi"} {
		if strings.TrimSpace(candidate) != "" {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no editor configured")
}

func envValue(env []string, key string) string {
	items := env
	if items == nil {
		items = os.Environ()
	}
	prefix := key + "="
	for _, item := range items {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}

func selectedConfigPath(flags configCommandFlags, env []string) string {
	if flags.configPath != "" {
		return flags.configPath
	}
	return envValue(env, "VIDEODL_CONFIG")
}
