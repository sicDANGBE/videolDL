package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"video-downloader/internal/config"
)

type daemonLaunchRequest struct {
	executable string
	args       []string
	env        []string
	logPath    string
}

type daemonState struct {
	pid     int
	status  string
	message string
}

type daemonStartLock struct {
	path string
	file *os.File
}

type daemonStartLockState struct {
	exists bool
	active bool
}

var daemonLaunchProcess = startDetachedDaemonProcess

func acquireDaemonStartLock(ctx context.Context, pidPath string) (*daemonStartLock, error) {
	lockPath := pidPath + ".start.lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("create daemon start lock directory: %w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("acquire daemon start lock: %w", err)
		}
		lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if _, err := fmt.Fprintf(lockFile, "%d\n", os.Getpid()); err != nil {
				closeErr := lockFile.Close()
				removeErr := os.Remove(lockPath)
				return nil, fmt.Errorf("write daemon start lock: %w", errors.Join(err, closeErr, removeErr))
			}
			return &daemonStartLock{path: lockPath, file: lockFile}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create daemon start lock: %w", err)
		}
		state := inspectDaemonStartLock(lockPath)
		if state.exists && !state.active {
			if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("remove stale daemon start lock: %w", err)
			}
			continue
		}
		if !state.exists {
			continue
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil, fmt.Errorf("acquire daemon start lock: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func inspectDaemonStartLock(path string) daemonStartLockState {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return daemonStartLockState{}
	}
	if err != nil {
		return daemonStartLockState{exists: true, active: true}
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return daemonStartLockState{exists: true, active: true}
	}
	return daemonStartLockState{exists: true, active: processExists(pid) && processMatchesExecutable(pid)}
}

func (lock *daemonStartLock) release() error {
	closeErr := lock.file.Close()
	removeErr := os.Remove(lock.path)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	if closeErr != nil || removeErr != nil {
		return fmt.Errorf("release daemon start lock: %w", errors.Join(closeErr, removeErr))
	}
	return nil
}

func newDaemonLaunchRequest(options Options, flags daemonFlags, loaded config.Config) (daemonLaunchRequest, error) {
	executable, err := os.Executable()
	if err != nil {
		return daemonLaunchRequest{}, fmt.Errorf("resolve executable: %w", err)
	}
	args := []string{"__daemon-worker"}
	if flags.configPath != "" {
		args = append(args, "--config", loaded.ConfigPath)
	}
	return daemonLaunchRequest{executable: executable, args: args, env: daemonEnvironment(options.Env), logPath: loaded.DaemonLogPath}, nil
}

func startDetachedDaemonProcess(ctx context.Context, request daemonLaunchRequest) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("start daemon: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(request.logPath), 0o755); err != nil {
		return 0, fmt.Errorf("create daemon log directory: %w", err)
	}
	logFile, err := os.OpenFile(request.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open daemon log: %w", err)
	}
	defer logFile.Close()
	command := exec.Command(request.executable, request.args...)
	command.Env = request.env
	command.Stdout = logFile
	command.Stderr = logFile
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return 0, fmt.Errorf("start daemon process: %w", err)
	}
	pid := command.Process.Pid
	return pid, command.Process.Release()
}

func daemonEnvironment(env []string) []string {
	if env == nil {
		return os.Environ()
	}
	return append([]string(nil), env...)
}

func writeDaemonPID(path string, pid int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create daemon pid directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		return fmt.Errorf("write daemon pid file: %w", err)
	}
	return nil
}

func inspectDaemon(pidPath string) daemonState {
	data, err := os.ReadFile(pidPath)
	if errors.Is(err, os.ErrNotExist) {
		return daemonState{status: "stopped"}
	}
	if err != nil {
		return daemonState{status: "stale", message: err.Error()}
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return daemonState{status: "stale", message: "invalid pid file"}
	}
	if !processExists(pid) {
		return daemonState{pid: pid, status: "stale", message: "process is not running"}
	}
	if !processMatchesExecutable(pid) {
		return daemonState{pid: pid, status: "stale", message: "process is not videodl"}
	}
	return daemonState{pid: pid, status: "running"}
}

func processExists(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return true
	}
	fields := strings.Fields(string(data))
	return len(fields) < 3 || fields[2] != "Z"
}

func processMatchesExecutable(pid int) bool {
	current, err := os.Executable()
	if err != nil {
		return false
	}
	current, err = filepath.EvalSymlinks(current)
	if err != nil {
		return false
	}
	target, err := filepath.EvalSymlinks(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	return err == nil && target == current
}
