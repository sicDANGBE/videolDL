package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestRun_daemonStatus_reports_stopped_running_and_stale(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	pidPath := filepath.Join(home, ".local", "state", "videodl", "daemon.pid")
	process := startDaemonHelperProcess(t)

	// When
	stopped := runDaemonSubcommand(t, home, workDir, "status")
	writeTestDaemonPID(t, pidPath, process.Process.Pid)
	running := runDaemonSubcommand(t, home, workDir, "status")
	stopHelperProcess(t, process)
	stale := runDaemonSubcommand(t, home, workDir, "status")

	// Then
	if !strings.Contains(stopped, "stopped") {
		t.Fatalf("stopped status = %q", stopped)
	}
	if !strings.Contains(running, "running") || !strings.Contains(running, strconv.Itoa(process.Process.Pid)) {
		t.Fatalf("running status = %q", running)
	}
	if !strings.Contains(stale, "stale") {
		t.Fatalf("stale status = %q", stale)
	}
}

func TestRun_daemonStart_refuses_duplicate_active_daemon(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	var launches int
	restore := replaceDaemonLauncher(func(context.Context, daemonLaunchRequest) (int, error) {
		launches++
		return os.Getpid(), nil
	})
	defer restore()

	// When
	first := runDaemonSubcommand(t, home, workDir, "start")
	secondErr := Run(context.Background(), Options{Args: []string{"daemon", "start"}, Out: ioDiscard{}, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}})

	// Then
	if secondErr == nil {
		t.Fatal("second daemon start unexpectedly succeeded")
	}
	if launches != 1 {
		t.Fatalf("launches = %d, want 1", launches)
	}
	if !strings.Contains(first, "started") {
		t.Fatalf("start output = %q", first)
	}
}

func TestRun_daemonStart_serializes_racing_starts(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	pidPath := filepath.Join(home, ".local", "state", "videodl", "daemon.pid")
	firstLaunchEntered := make(chan struct{})
	secondLaunchEntered := make(chan struct{}, 1)
	releaseFirstLaunch := make(chan struct{})
	var launches atomic.Int32
	restore := replaceDaemonLauncher(func(context.Context, daemonLaunchRequest) (int, error) {
		launch := launches.Add(1)
		if launch == 1 {
			close(firstLaunchEntered)
			<-releaseFirstLaunch
			return os.Getpid(), nil
		}
		secondLaunchEntered <- struct{}{}
		<-releaseFirstLaunch
		return os.Getpid() + int(launch), nil
	})
	defer restore()
	results := make(chan error, 2)
	start := func() {
		results <- Run(context.Background(), Options{Args: []string{"daemon", "start"}, Out: ioDiscard{}, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}})
	}

	// When
	go start()
	<-firstLaunchEntered
	go start()

	// Then
	select {
	case <-secondLaunchEntered:
		close(releaseFirstLaunch)
		firstErr := <-results
		secondErr := <-results
		t.Fatalf("racing daemon starts launched twice; first error = %v, second error = %v", firstErr, secondErr)
	case <-time.After(200 * time.Millisecond):
		close(releaseFirstLaunch)
	}
	firstErr := <-results
	secondErr := <-results
	if firstErr == nil && secondErr == nil {
		t.Fatal("both daemon starts succeeded, want one duplicate-running refusal")
	}
	if firstErr != nil && secondErr != nil {
		t.Fatalf("both daemon starts failed: first = %v, second = %v", firstErr, secondErr)
	}
	for _, err := range []error{firstErr, secondErr} {
		if err != nil && !strings.Contains(err.Error(), "daemon already running") {
			t.Fatalf("duplicate daemon start error = %v, want daemon already running", err)
		}
	}
	if launches.Load() != 1 {
		t.Fatalf("launches = %d, want 1", launches.Load())
	}
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read pid file after racing starts: %v", err)
	}
	if strings.TrimSpace(string(data)) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("pid file = %q, want %d", data, os.Getpid())
	}
}

func TestRun_daemonStop_targets_only_recorded_videodl_pid(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	pidPath := filepath.Join(home, ".local", "state", "videodl", "daemon.pid")
	unrelated := exec.Command("/bin/sleep", "30")
	if err := unrelated.Start(); err != nil {
		t.Fatalf("start unrelated process: %v", err)
	}
	t.Cleanup(func() {
		_ = unrelated.Process.Kill()
		_, _ = unrelated.Process.Wait()
	})
	writeTestDaemonPID(t, pidPath, unrelated.Process.Pid)

	// When
	err := Run(context.Background(), Options{Args: []string{"daemon", "stop"}, Out: ioDiscard{}, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}})

	// Then
	if err == nil {
		t.Fatal("daemon stop unexpectedly targeted unrelated process")
	}
	if signalErr := unrelated.Process.Signal(syscall.Signal(0)); signalErr != nil {
		t.Fatalf("unrelated process was not left running: %v", signalErr)
	}
}

func TestRun_daemonStop_stops_recorded_daemon_pid(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	pidPath := filepath.Join(home, ".local", "state", "videodl", "daemon.pid")
	process := startDaemonHelperProcess(t)
	writeTestDaemonPID(t, pidPath, process.Process.Pid)

	// When
	output := runDaemonSubcommand(t, home, workDir, "stop")

	// Then
	if !strings.Contains(output, "stopped") {
		t.Fatalf("stop output = %q", output)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file after stop stat error = %v", err)
	}
}

func TestRun_daemonRestart_stops_then_starts(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	pidPath := filepath.Join(home, ".local", "state", "videodl", "daemon.pid")
	process := startDaemonHelperProcess(t)
	writeTestDaemonPID(t, pidPath, process.Process.Pid)
	restore := replaceDaemonLauncher(func(context.Context, daemonLaunchRequest) (int, error) { return 5252, nil })
	defer restore()

	// When
	output := runDaemonSubcommand(t, home, workDir, "restart")

	// Then
	if !strings.Contains(output, "stopped") || !strings.Contains(output, "started") {
		t.Fatalf("restart output = %q", output)
	}
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read pid file after restart: %v", err)
	}
	if strings.TrimSpace(string(data)) != "5252" {
		t.Fatalf("pid file = %q, want 5252", data)
	}
}

func TestRun_daemonLogs_uses_configured_log_path(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	logPath := filepath.Join(home, ".local", "state", "videodl", "logs", "daemon.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("create log directory: %v", err)
	}
	if err := os.WriteFile(logPath, []byte("daemon line\n"), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}

	// When
	output := runDaemonSubcommand(t, home, workDir, "logs")

	// Then
	if !strings.Contains(output, logPath) || !strings.Contains(output, "daemon line") {
		t.Fatalf("logs output = %q", output)
	}
}

func runDaemonSubcommand(t *testing.T, home string, workDir string, subcommand string) string {
	t.Helper()
	var output bytes.Buffer
	err := Run(context.Background(), Options{Args: []string{"daemon", subcommand}, Out: &output, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}})
	if err != nil {
		t.Fatalf("daemon %s returned error: %v", subcommand, err)
	}
	return output.String()
}

func writeTestDaemonPID(t *testing.T, path string, pid int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create pid directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(fmt.Sprintf("%d\n", pid)), 0o600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}
}

func replaceDaemonLauncher(launcher func(context.Context, daemonLaunchRequest) (int, error)) func() {
	previous := daemonLaunchProcess
	daemonLaunchProcess = launcher
	return func() { daemonLaunchProcess = previous }
}

func startDaemonHelperProcess(t *testing.T) *exec.Cmd {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=TestDaemonHelperProcess")
	command.Env = append(os.Environ(), "VIDEODL_DAEMON_HELPER=1")
	if err := command.Start(); err != nil {
		t.Fatalf("start daemon helper: %v", err)
	}
	t.Cleanup(func() { stopHelperProcess(t, command) })
	return command
}

func stopHelperProcess(t *testing.T, command *exec.Cmd) {
	t.Helper()
	if command.Process == nil {
		return
	}
	_ = command.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = command.Process.Kill()
		<-done
	}
}

func TestDaemonHelperProcess(t *testing.T) {
	if os.Getenv("VIDEODL_DAEMON_HELPER") != "1" {
		return
	}
	select {}
}
