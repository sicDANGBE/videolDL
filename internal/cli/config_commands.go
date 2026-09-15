package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
)

type configCommandFlags struct {
	configPath string
	editor     string
	json       bool
	help       bool
}

type persistedConfig struct {
	IdleTimeout     string `json:"idle_timeout"`
	MaxHeight       int    `json:"max_height"`
	Resume          bool   `json:"resume"`
	Retries         int    `json:"retries"`
	StatePath       string `json:"state_path"`
	LogPath         string `json:"log_path"`
	Destination     string `json:"destination"`
	Concurrency     int    `json:"concurrency"`
	Timeout         string `json:"timeout"`
	FFmpeg          bool   `json:"ffmpeg"`
	FFmpegPath      string `json:"ffmpeg_path"`
	WebhookURL      string `json:"webhook_url"`
	Editor          string `json:"editor"`
	AutoStartWorker bool   `json:"auto_start_worker"`
	DaemonPIDPath   string `json:"daemon_pid_path"`
	DaemonLogPath   string `json:"daemon_log_path"`
	NotifyCommand   string `json:"notify_command"`
}

func runConfigCommand(ctx context.Context, options Options, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: videodl config init|path|show|get|set|edit")
	}
	if args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprintln(options.Out, "Usage: videodl config init|path|show|get|set|edit\n\nSubcommands:\n  config init [flags]\n  config path [flags]\n  config show [flags]\n  config get [flags] KEY\n  config set [flags] KEY VALUE\n  config edit [flags]")
		return err
	}
	subcommand := args[0]
	flags, positional, err := parseConfigFlags(subcommand, args[1:], options)
	if err != nil || flags.help {
		return err
	}
	workingDir, err := configWorkingDir(options)
	if err != nil {
		return err
	}
	switch subcommand {
	case "init":
		return configInit(options, flags, workingDir, positional)
	case "path":
		return configPath(options, flags, workingDir, positional)
	case "show":
		return configShow(options, flags, workingDir, positional)
	case "get":
		return configGet(options, flags, workingDir, positional)
	case "set":
		return configSet(options, flags, workingDir, positional)
	case "edit":
		return configEdit(ctx, options, flags, workingDir, positional)
	default:
		return fmt.Errorf("unknown config command %q", subcommand)
	}
}

func parseConfigFlags(name string, args []string, options Options) (configCommandFlags, []string, error) {
	flags := configCommandFlags{}
	set := flag.NewFlagSet("videodl config "+name, flag.ContinueOnError)
	set.SetOutput(options.Out)
	set.Usage = func() {
		_, _ = fmt.Fprintf(options.Out, "Usage: videodl config %s [flags]\n\nOptions:\n", name)
		set.PrintDefaults()
	}
	set.StringVar(&flags.configPath, "config", "", "chemin du fichier de configuration")
	set.BoolVar(&flags.json, "json", false, "émettre du JSON")
	set.BoolVar(&flags.help, "help", false, "afficher cette aide")
	if name == "edit" {
		set.StringVar(&flags.editor, "editor", "", "éditeur à utiliser pour cette invocation")
	}
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.help = true
			return flags, nil, nil
		}
		return configCommandFlags{}, nil, err
	}
	if flags.help {
		set.Usage()
	}
	return flags, set.Args(), nil
}

func configInit(options Options, flags configCommandFlags, workingDir string, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: videodl config init [flags]")
	}
	path, loaded, err := loadForConfigCommand(options, flags, workingDir, false)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("config file %q already exists", path)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat config file %q: %w", path, err)
	}
	if err := writePersistedConfig(path, fromConfig(loaded)); err != nil {
		return err
	}
	if err := writeSetupFiles(path, loaded); err != nil {
		return err
	}
	_, err = fmt.Fprintf(options.Out, "created %s\nGuide et autocomplétion : même dossier. Suite : videodl doctor\n", path)
	return err
}

func configPath(options Options, flags configCommandFlags, workingDir string, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: videodl config path [flags]")
	}
	path, _, err := loadForConfigCommand(options, flags, workingDir, false)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(options.Out, path)
	return err
}

func configShow(options Options, flags configCommandFlags, workingDir string, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: videodl config show [flags]")
	}
	_, loaded, err := loadForConfigCommand(options, flags, workingDir, true)
	if err != nil {
		return err
	}
	stored := fromConfig(loaded)
	if stored.WebhookURL != "" {
		stored.WebhookURL = "[configured]"
	}
	if stored.NotifyCommand != "" {
		stored.NotifyCommand = "[configured]"
	}
	if flags.json {
		return json.NewEncoder(options.Out).Encode(stored)
	}
	_, err = fmt.Fprint(options.Out, formatConfig(stored))
	return err
}

func configGet(options Options, flags configCommandFlags, workingDir string, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: videodl config get [flags] KEY")
	}
	_, loaded, err := loadForConfigCommand(options, flags, workingDir, true)
	if err != nil {
		return err
	}
	value, err := configValue(fromConfig(loaded), args[0])
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(options.Out, value)
	return err
}

func configSet(options Options, flags configCommandFlags, workingDir string, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: videodl config set [flags] KEY VALUE")
	}
	path, loaded, err := loadForConfigCommand(options, flags, workingDir, false)
	if err != nil {
		return err
	}
	stored := fromConfig(loaded)
	if err := setConfigValue(&stored, args[0], args[1], workingDir); err != nil {
		return err
	}
	if args[0] == "destination" {
		if err := os.MkdirAll(stored.Destination, 0o755); err != nil {
			return fmt.Errorf("create destination: %w", err)
		}
	}
	if err := updateConfigFile(path, stored, options, flags, workingDir); err != nil {
		return err
	}
	_, err = fmt.Fprintf(options.Out, "%s=%s\n", args[0], mustConfigValue(stored, args[0]))
	return err
}

func configEdit(ctx context.Context, options Options, flags configCommandFlags, workingDir string, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: videodl config edit [flags]")
	}
	path, loaded, err := loadForConfigCommand(options, flags, workingDir, false)
	if err != nil {
		return err
	}
	if err := ensureConfigFile(path, fromConfig(loaded)); err != nil {
		return err
	}
	before, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config before edit %q: %w", path, err)
	}
	editor, err := selectEditor(flags.editor, loaded.Editor, options.Env)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, editor, path)
	command.Stdin = os.Stdin
	command.Stdout = options.Out
	command.Stderr = options.ErrOut
	if err := command.Run(); err != nil {
		restoreErr := writeConfigAtomic(path, before)
		return errors.Join(fmt.Errorf("run editor %q: %w", editor, err), restoreErr)
	}
	if _, _, err := loadForConfigCommand(options, flags, workingDir, true); err != nil {
		restoreErr := writeConfigAtomic(path, before)
		return errors.Join(fmt.Errorf("validate edited config: %w", err), restoreErr)
	}
	_, err = fmt.Fprintf(options.Out, "edited %s\n", path)
	return err
}
