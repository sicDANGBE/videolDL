package installer

import (
	"bytes"
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
	"video-downloader/internal/queue"
)

type service struct {
	pid       int
	token     string
	args, env []string
	cwd       string
	config    config.Config
}

func processToken(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	// comm can contain spaces and parentheses; fields after the final ')' start at state.
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return "", fmt.Errorf("état du processus illisible")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return "", fmt.Errorf("état du processus incomplet")
	}
	if fields[0] == "Z" || fields[0] == "X" {
		return "", os.ErrNotExist
	}
	return fields[19], nil
}
func processPath(pid int) (string, error) {
	value, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	return strings.TrimSuffix(value, " (deleted)"), err
}
func (s service) alive() bool {
	token, err := processToken(s.pid)
	return err == nil && token == s.token
}

// Match the complete canonical installation path, including an inode that was
// replaced by an earlier installer. Never identify a process by basename alone.
func servicesAt(executable string) ([]service, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var found []service
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		path, err := processPath(pid)
		if err != nil || path != executable {
			continue
		}
		token, err := processToken(pid)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		proc := fmt.Sprintf("/proc/%d", pid)
		info, err := os.Stat(proc)
		if err != nil {
			return nil, err
		}
		if info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
			return nil, fmt.Errorf("processus %d appartenant à un autre utilisateur : mise à jour interrompue", pid)
		}
		command, err := os.ReadFile(proc + "/cmdline")
		if err != nil {
			return nil, err
		}
		args := strings.Split(strings.TrimRight(string(command), "\x00"), "\x00")
		if len(args) < 2 || args[1] != "__daemon-worker" {
			return nil, fmt.Errorf("processus videodl au premier plan (PID %d) : attendre sa fin ou l’arrêter avant la mise à jour", pid)
		}
		configPath := ""
		switch {
		case len(args) == 2:
		case len(args) == 4 && args[2] == "--config":
			configPath = args[3]
		case len(args) == 3 && strings.HasPrefix(args[2], "--config="):
			configPath = strings.TrimPrefix(args[2], "--config=")
		default:
			return nil, fmt.Errorf("options du service %d non reconnues : mise à jour interrompue", pid)
		}
		environment, err := os.ReadFile(proc + "/environ")
		if err != nil {
			return nil, fmt.Errorf("lire le contexte du service %d : %w", pid, err)
		}
		env := strings.Split(strings.TrimRight(string(environment), "\x00"), "\x00")
		cwd, err := os.Readlink(proc + "/cwd")
		if err != nil {
			return nil, err
		}
		if configPath != "" && !filepath.IsAbs(configPath) {
			configPath = filepath.Join(cwd, configPath)
		}
		home := ""
		for _, item := range env {
			if strings.HasPrefix(item, "HOME=") {
				home = strings.TrimPrefix(item, "HOME=")
			}
		}
		// Configuration contents and the private environment are never printed.
		loadEnv := append([]string(nil), env...)
		for i, item := range loadEnv {
			if strings.HasPrefix(item, "VIDEODL_CONFIG=") {
				value := strings.TrimPrefix(item, "VIDEODL_CONFIG=")
				if value != "" && !filepath.IsAbs(value) {
					loadEnv[i] = "VIDEODL_CONFIG=" + filepath.Join(cwd, value)
				}
			}
		}
		loaded, err := config.Load(config.LoadOptions{ConfigPath: configPath, HomeDir: home, WorkingDir: cwd, Env: loadEnv})
		if err != nil {
			return nil, fmt.Errorf("configuration du service %d invalide : vérifier avec videodl doctor", pid)
		}
		recorded, err := readPID(loaded.DaemonPIDPath)
		if err != nil || recorded != pid {
			return nil, fmt.Errorf("PID du service %d non confirmé par sa configuration : mise à jour interrompue", pid)
		}
		if latest, err := processToken(pid); err != nil || latest != token {
			return nil, fmt.Errorf("le service %d a changé pendant la vérification ; réessayer", pid)
		}
		found = append(found, service{pid: pid, token: token, args: append([]string(nil), args[2:]...), env: env, cwd: cwd, config: loaded})
	}
	return found, nil
}
func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("PID invalide")
	}
	return pid, nil
}
func stopService(ctx context.Context, s service) error {
	if !s.alive() {
		return nil
	}
	process, err := os.FindProcess(s.pid)
	if err != nil {
		return err
	}
	defer process.Release()
	if !s.alive() {
		return nil
	}
	if err := process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	timer := time.NewTicker(25 * time.Millisecond)
	defer timer.Stop()
	for s.alive() {
		select {
		case <-ctx.Done():
			return fmt.Errorf("arrêt du service %d non terminé : %w", s.pid, ctx.Err())
		case <-timer.C:
		}
	}
	return nil
}
func startService(ctx context.Context, executable string, s service) (service, error) {
	if pid, err := readPID(s.config.DaemonPIDPath); err == nil {
		if _, err := processToken(pid); err == nil {
			return service{}, fmt.Errorf("un processus est déjà enregistré pour %s", s.config.DaemonPIDPath)
		}
	}
	store, err := queue.NewStore(s.config.StatePath)
	if err != nil {
		return service{}, err
	}
	release, err := store.LockSupervisor()
	if err != nil {
		return service{}, err
	}
	release()
	if err := os.MkdirAll(filepath.Dir(s.config.DaemonLogPath), 0755); err != nil {
		return service{}, err
	}
	log, err := os.OpenFile(s.config.DaemonLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return service{}, err
	}
	defer log.Close()
	args := append([]string{"__daemon-worker"}, s.args...)
	command := exec.Command(executable, args...)
	command.Env = s.env
	command.Dir = s.cwd
	command.Stdout = log
	command.Stderr = log
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := ctx.Err(); err != nil {
		return service{}, err
	}
	if err := command.Start(); err != nil {
		return service{}, fmt.Errorf("relancer le service : %w", err)
	}
	next := s
	next.pid = command.Process.Pid
	// Retain the process handle until its identity has been captured, then reap
	// its eventual exit without keeping the installer waiting for the service.
	next.token, err = processToken(next.pid)
	if err != nil {
		command.Process.Kill()
		command.Wait()
		return service{}, fmt.Errorf("le service s’est arrêté immédiatement")
	}
	go command.Wait()
	pidDir := filepath.Dir(s.config.DaemonPIDPath)
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		return next, err
	}
	temporary, err := copyToTemp(pidDir, bytes.NewBufferString(strconv.Itoa(next.pid)+"\n"), 0600)
	if err != nil {
		return next, err
	}
	defer os.Remove(temporary)
	if err := os.Rename(temporary, s.config.DaemonPIDPath); err != nil {
		return next, err
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		path, pathErr := processPath(next.pid)
		if !next.alive() || pathErr != nil || path != executable {
			return next, fmt.Errorf("le nouveau service n’est pas actif ; consulter videodl daemon logs")
		}
		release, lockErr := store.LockSupervisor()
		if errors.Is(lockErr, queue.ErrSupervisorRunning) {
			return next, nil
		}
		if lockErr != nil {
			return next, lockErr
		}
		release()
		select {
		case <-ctx.Done():
			return next, fmt.Errorf("le nouveau service ne prend pas la file en charge : %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
