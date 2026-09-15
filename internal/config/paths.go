package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type defaultPathSet struct {
	Config      string
	State       string
	LogDir      string
	Destination string
	DaemonPID   string
	DaemonLog   string
}

func defaultHome(homeDir string) (string, error) {
	if homeDir != "" {
		return homeDir, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return homeDir, nil
}

func xdgPath(env []string, key, fallback string) string {
	for _, item := range env {
		if len(item) > len(key)+1 && item[:len(key)] == key && item[len(key)] == '=' {
			if item[len(key)+1:] != "" {
				return item[len(key)+1:]
			}
		}
	}
	return fallback
}

func defaultPaths(homeDir string, env []string) (defaultPathSet, error) {
	homeDir, err := defaultHome(homeDir)
	if err != nil {
		return defaultPathSet{}, err
	}
	configHome := xdgPath(env, "XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))
	stateHome := xdgPath(env, "XDG_STATE_HOME", filepath.Join(homeDir, ".local", "state"))
	base := filepath.Join("videodl")
	logDir := filepath.Join(stateHome, base, "logs")
	return defaultPathSet{
		Config:      filepath.Join(configHome, base, "config.json"),
		State:       filepath.Join(stateHome, base, "queue.json"),
		LogDir:      logDir,
		Destination: filepath.Join(homeDir, "Downloads", "videodl"),
		DaemonPID:   filepath.Join(stateHome, base, "daemon.pid"),
		DaemonLog:   filepath.Join(logDir, "daemon.log"),
	}, nil
}

func ResolveDestination(destination, workingDir string) (string, error) {
	if destination == "" {
		return "", &ValidationError{Field: "destination", Rule: "must not be empty"}
	}
	if workingDir == "" {
		var err error
		workingDir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve working directory: %w", err)
		}
	}
	if !filepath.IsAbs(workingDir) {
		return "", &ValidationError{Field: "working_dir", Rule: "must be absolute"}
	}
	resolved := destination
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(workingDir, resolved)
	}
	resolved, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve destination: %w", err)
	}
	resolved = filepath.Clean(resolved)
	if !filepath.IsAbs(destination) {
		base := filepath.Clean(workingDir)
		relative, err := filepath.Rel(base, resolved)
		if err != nil {
			return "", fmt.Errorf("compare destination with working directory: %w", err)
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", &ValidationError{Field: "destination", Rule: "must remain within working directory"}
		}
	}
	return resolved, nil
}
