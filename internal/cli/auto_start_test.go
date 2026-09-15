package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRun_add_does_not_start_daemon_when_auto_start_disabled(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	configPath := filepath.Join(workDir, "config.json")
	writeAutoStartConfig(t, configPath, workDir, false)
	var launches int
	restore := replaceDaemonLauncher(func(context.Context, daemonLaunchRequest) (int, error) {
		launches++
		return os.Getpid(), nil
	})
	defer restore()

	// When
	err := Run(context.Background(), Options{Args: []string{"add", "--config", configPath, "--name", "clip.webm", "https://example.test/clip.webm"}, Out: ioDiscard{}, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}})

	// Then
	if err != nil {
		t.Fatalf("add returned error: %v", err)
	}
	if launches != 0 {
		t.Fatalf("daemon launches = %d, want 0", launches)
	}
}

func TestRun_add_starts_daemon_when_auto_start_enabled(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	configPath := filepath.Join(workDir, "config.json")
	pipeline := writeAutoStartConfig(t, configPath, workDir, true)
	var launches int
	restore := replaceDaemonLauncher(func(context.Context, daemonLaunchRequest) (int, error) {
		launches++
		return os.Getpid(), nil
	})
	defer restore()

	// When
	err := Run(context.Background(), Options{Args: []string{"add", "--config", configPath, "--name", "clip.webm", "https://example.test/clip.webm"}, Out: ioDiscard{}, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}})

	// Then
	if err != nil {
		t.Fatalf("add returned error: %v", err)
	}
	if launches != 1 {
		t.Fatalf("daemon launches = %d, want 1", launches)
	}
	data, err := os.ReadFile(pipeline.pidPath)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	if strings.TrimSpace(string(data)) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("pid file = %q, want current pid", data)
	}
}

func TestRun_add_does_not_start_duplicate_daemon_when_auto_start_enabled(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	configPath := filepath.Join(workDir, "config.json")
	pipeline := writeAutoStartConfig(t, configPath, workDir, true)
	writeTestDaemonPID(t, pipeline.pidPath, os.Getpid())
	var launches int
	restore := replaceDaemonLauncher(func(context.Context, daemonLaunchRequest) (int, error) {
		launches++
		return os.Getpid(), nil
	})
	defer restore()

	// When
	err := Run(context.Background(), Options{Args: []string{"add", "--config", configPath, "--name", "clip.webm", "https://example.test/clip.webm"}, Out: ioDiscard{}, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}})

	// Then
	if err != nil {
		t.Fatalf("add returned error: %v", err)
	}
	if launches != 0 {
		t.Fatalf("daemon launches = %d, want 0", launches)
	}
}

type autoStartConfigPaths struct {
	pidPath string
}

func writeAutoStartConfig(t *testing.T, configPath string, workDir string, enabled bool) autoStartConfigPaths {
	t.Helper()
	statePath := filepath.Join(workDir, "queue.json")
	logPath := filepath.Join(workDir, "logs")
	pidPath := filepath.Join(workDir, "daemon.pid")
	daemonLogPath := filepath.Join(workDir, "daemon.log")
	configJSON := fmt.Sprintf(`{
		"state_path": %q,
		"log_path": %q,
		"destination": %q,
		"auto_start_worker": %t,
		"daemon_pid_path": %q,
		"daemon_log_path": %q
	}`, statePath, logPath, workDir, enabled, pidPath, daemonLogPath)
	if err := os.WriteFile(configPath, []byte(configJSON), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return autoStartConfigPaths{pidPath: pidPath}
}
