package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"syscall"

	"video-downloader/internal/config"
)

type daemonFlags struct {
	configPath string
	help       bool
}

type daemonStartRequest struct {
	options      Options
	flags        daemonFlags
	config       config.Config
	allowRunning bool
	out          io.Writer
}

func runDaemonCommand(ctx context.Context, options Options, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: videodl daemon start|stop|status|restart|logs")
	}
	if args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprintln(options.Out, "Usage: videodl daemon start|stop|status|restart|logs\n\nSubcommands:\n  daemon start [flags]\n  daemon stop [flags]\n  daemon status [flags]\n  daemon restart [flags]\n  daemon logs [flags]")
		return err
	}
	subcommand := args[0]
	flags, positional, err := parseDaemonFlags(subcommand, args[1:], options)
	if err != nil || flags.help {
		return err
	}
	workingDir, err := configWorkingDir(options)
	if err != nil {
		return err
	}
	loaded, err := loadDaemonConfig(options, flags, workingDir)
	if err != nil {
		return err
	}
	if len(positional) != 0 {
		return fmt.Errorf("usage: videodl daemon %s [flags]", subcommand)
	}
	switch subcommand {
	case "start":
		return daemonStart(ctx, options, flags, loaded)
	case "stop":
		return daemonStop(options.Out, loaded)
	case "status":
		return daemonStatus(options.Out, loaded)
	case "restart":
		if err := daemonStop(options.Out, loaded); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return daemonStart(ctx, options, flags, loaded)
	case "logs":
		return daemonLogs(options.Out, loaded)
	default:
		return fmt.Errorf("unknown daemon command %q", subcommand)
	}
}

func parseDaemonFlags(name string, args []string, options Options) (daemonFlags, []string, error) {
	flags := daemonFlags{}
	set := flag.NewFlagSet("videodl daemon "+name, flag.ContinueOnError)
	set.SetOutput(options.Out)
	set.Usage = func() {
		_, _ = fmt.Fprintf(options.Out, "Usage: videodl daemon %s [flags]\n\nOptions:\n", name)
		set.PrintDefaults()
	}
	set.StringVar(&flags.configPath, "config", "", "chemin du fichier de configuration")
	set.BoolVar(&flags.help, "help", false, "afficher cette aide")
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.help = true
			return flags, nil, nil
		}
		return daemonFlags{}, nil, err
	}
	if flags.help {
		set.Usage()
	}
	return flags, set.Args(), nil
}

func loadDaemonConfig(options Options, flags daemonFlags, workingDir string) (config.Config, error) {
	overrides := config.Overrides{}
	if flags.configPath != "" {
		overrides.ConfigPath = flags.configPath
	}
	loaded, err := config.Load(config.LoadOptions{ConfigPath: flags.configPath, HomeDir: options.HomeDir, WorkingDir: workingDir, Env: options.Env, Overrides: overrides})
	if err != nil {
		return config.Config{}, err
	}
	return loaded, nil
}

func daemonStart(ctx context.Context, options Options, flags daemonFlags, loaded config.Config) error {
	return daemonStartWithPolicy(ctx, daemonStartRequest{options: options, flags: flags, config: loaded, out: options.Out})
}

func daemonStartWhenStopped(ctx context.Context, options Options, flags daemonFlags, loaded config.Config) error {
	return daemonStartWithPolicy(ctx, daemonStartRequest{options: options, flags: flags, config: loaded, allowRunning: true})
}

func daemonStartWithPolicy(ctx context.Context, request daemonStartRequest) (err error) {
	lock, err := acquireDaemonStartLock(ctx, request.config.DaemonPIDPath)
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := lock.release(); err == nil && releaseErr != nil {
			err = releaseErr
		}
	}()

	state := inspectDaemon(request.config.DaemonPIDPath)
	if state.status == "running" {
		if request.allowRunning {
			return nil
		}
		return fmt.Errorf("daemon already running with pid %d", state.pid)
	}
	if state.status == "stale" && state.message == "process is not videodl" {
		return fmt.Errorf("pid file %s points to non-videodl process %d", request.config.DaemonPIDPath, state.pid)
	}
	if state.status == "stale" {
		if err := os.Remove(request.config.DaemonPIDPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove stale daemon pid file: %w", err)
		}
	}
	launchRequest, err := newDaemonLaunchRequest(request.options, request.flags, request.config)
	if err != nil {
		return err
	}
	pid, err := daemonLaunchProcess(ctx, launchRequest)
	if err != nil {
		return err
	}
	if err := writeDaemonPID(request.config.DaemonPIDPath, pid); err != nil {
		return err
	}
	if request.out == nil {
		return nil
	}
	_, err = fmt.Fprintf(request.out, "started pid=%d log=%s\n", pid, request.config.DaemonLogPath)
	return err
}

func daemonStop(out io.Writer, loaded config.Config) error {
	state := inspectDaemon(loaded.DaemonPIDPath)
	switch state.status {
	case "stopped":
		_, err := fmt.Fprintln(out, "stopped")
		return err
	case "stale":
		if state.message == "process is not videodl" {
			return fmt.Errorf("refusing to stop non-videodl process %d", state.pid)
		}
		if err := os.Remove(loaded.DaemonPIDPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove stale daemon pid file: %w", err)
		}
		_, err := fmt.Fprintf(out, "stopped stale pid=%d\n", state.pid)
		return err
	case "running":
		if state.pid == os.Getpid() {
			return fmt.Errorf("refusing to stop current process pid %d", state.pid)
		}
		process, err := os.FindProcess(state.pid)
		if err != nil {
			return fmt.Errorf("find daemon process %d: %w", state.pid, err)
		}
		if err := process.Signal(syscall.SIGTERM); err != nil {
			return fmt.Errorf("stop daemon process %d: %w", state.pid, err)
		}
		if err := os.Remove(loaded.DaemonPIDPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove daemon pid file: %w", err)
		}
		_, err = fmt.Fprintf(out, "stopped pid=%d\n", state.pid)
		return err
	default:
		return fmt.Errorf("unknown daemon status %q", state.status)
	}
}

func daemonStatus(out io.Writer, loaded config.Config) error {
	state := inspectDaemon(loaded.DaemonPIDPath)
	if state.pid == 0 {
		_, err := fmt.Fprintf(out, "%s pid_file=%s\n", state.status, loaded.DaemonPIDPath)
		return err
	}
	_, err := fmt.Fprintf(out, "%s pid=%d pid_file=%s %s\n", state.status, state.pid, loaded.DaemonPIDPath, state.message)
	return err
}

func daemonLogs(out io.Writer, loaded config.Config) error {
	if _, err := fmt.Fprintf(out, "log=%s\n", loaded.DaemonLogPath); err != nil {
		return err
	}
	data, err := os.ReadFile(loaded.DaemonLogPath)
	if err == nil {
		_, writeErr := out.Write(data)
		return writeErr
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("read daemon log: %w", err)
}
