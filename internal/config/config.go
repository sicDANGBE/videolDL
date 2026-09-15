package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	IdleTimeout     time.Duration `json:"-"`
	MaxHeight       int           `json:"max_height"`
	Resume          bool          `json:"resume"`
	Retries         int           `json:"retries"`
	ConfigPath      string        `json:"-"`
	StatePath       string        `json:"state_path"`
	LogPath         string        `json:"log_path"`
	Destination     string        `json:"destination"`
	Concurrency     int           `json:"concurrency"`
	Timeout         time.Duration `json:"-"`
	FFmpeg          bool          `json:"ffmpeg"`
	FFmpegPath      string        `json:"ffmpeg_path"`
	WebhookURL      string        `json:"webhook_url"`
	Editor          string        `json:"editor"`
	AutoStartWorker bool          `json:"auto_start_worker"`
	DaemonPIDPath   string        `json:"daemon_pid_path"`
	DaemonLogPath   string        `json:"daemon_log_path"`
	NotifyCommand   string        `json:"notify_command"`
}

type Overrides struct {
	IdleTimeout     *time.Duration
	MaxHeight       *int
	Resume          *bool
	Retries         *int
	ConfigPath      string
	StatePath       *string
	LogPath         *string
	Destination     *string
	Concurrency     *int
	Timeout         *time.Duration
	FFmpeg          *bool
	FFmpegPath      *string
	WebhookURL      *string
	Editor          *string
	AutoStartWorker *bool
	DaemonPIDPath   *string
	DaemonLogPath   *string
	NotifyCommand   *string
}

type LoadOptions struct {
	ConfigPath string
	HomeDir    string
	WorkingDir string
	Env        []string
	Overrides  Overrides
}

func Defaults(homeDir, workingDir string) (Config, error) {
	paths, err := defaultPaths(homeDir, nil)
	if err != nil {
		return Config{}, err
	}
	return Config{Retries: 3, Resume: true, MaxHeight: 0, IdleTimeout: 60 * time.Second, ConfigPath: paths.Config, StatePath: paths.State, LogPath: paths.LogDir, Destination: paths.Destination, Concurrency: 1, Timeout: 30 * time.Second, DaemonPIDPath: paths.DaemonPID, DaemonLogPath: paths.DaemonLog}, nil
}

func Load(options LoadOptions) (Config, error) {
	env := options.Env
	if env == nil {
		env = os.Environ()
	}
	paths, err := defaultPaths(options.HomeDir, env)
	if err != nil {
		return Config{}, err
	}
	selectedConfigPath := options.ConfigPath
	if options.Overrides.ConfigPath != "" {
		selectedConfigPath = options.Overrides.ConfigPath
	}
	configPath, explicitlySelected := configFilePath(paths.Config, selectedConfigPath, env)
	config, err := Defaults(options.HomeDir, options.WorkingDir)
	if err != nil {
		return Config{}, err
	}
	config.ConfigPath, config.StatePath, config.LogPath = configPath, paths.State, paths.LogDir
	config.Destination, config.DaemonPIDPath, config.DaemonLogPath = paths.Destination, paths.DaemonPID, paths.DaemonLog
	if err := loadFile(&config, configPath, explicitlySelected); err != nil {
		return Config{}, err
	}
	if err := ApplyEnvironment(&config, env); err != nil {
		return Config{}, err
	}
	if err := ApplyOverrides(&config, options.Overrides); err != nil {
		return Config{}, err
	}
	return finalize(config, options.WorkingDir)
}

func configFilePath(defaultPath, option string, env []string) (string, bool) {
	if option != "" {
		return option, true
	}
	for _, item := range env {
		const key = "VIDEODL_CONFIG"
		if len(item) > len(key)+1 && item[:len(key)] == key && item[len(key)] == '=' && item[len(key)+1:] != "" {
			return item[len(key)+1:], true
		}
	}
	return defaultPath, false
}

func finalize(config Config, workingDir string) (Config, error) {
	var err error
	config.Destination, err = ResolveDestination(config.Destination, workingDir)
	if err != nil {
		return Config{}, err
	}
	for field, path := range map[string]string{"state_path": config.StatePath, "log_path": config.LogPath, "daemon_pid_path": config.DaemonPIDPath, "daemon_log_path": config.DaemonLogPath} {
		if path == "" {
			return Config{}, &ValidationError{Field: field, Rule: "must not be empty"}
		}
	}
	if config.StatePath, err = resolvePath(config.StatePath, workingDir, "state path"); err != nil {
		return Config{}, err
	}
	if config.LogPath, err = resolvePath(config.LogPath, workingDir, "log path"); err != nil {
		return Config{}, err
	}
	if config.DaemonPIDPath, err = resolvePath(config.DaemonPIDPath, workingDir, "daemon pid path"); err != nil {
		return Config{}, err
	}
	if config.DaemonLogPath, err = resolvePath(config.DaemonLogPath, workingDir, "daemon log path"); err != nil {
		return Config{}, err
	}
	return config, nil
}

func resolvePath(path, workingDir, label string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	resolved, err := filepath.Abs(filepath.Join(workingDir, path))
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", label, err)
	}
	return filepath.Clean(resolved), nil
}
