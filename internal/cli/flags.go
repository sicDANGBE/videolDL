package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"video-downloader/internal/config"
	"video-downloader/internal/downloader"
	"video-downloader/internal/observability"
	"video-downloader/internal/queue"
)

type commandFlags struct {
	editor                   string
	interval                 time.Duration
	once, nameSet, outputSet bool
	transferFlags
	jobOptions                                  downloader.Overrides
	configPath, statePath, logPath, destination string
	webhookURL, ffmpegPath                      string
	name, output                                string
	concurrency                                 int
	timeout                                     time.Duration
	ffmpeg, json, help, watch                   bool
	configSet, stateSet, logSet                 bool
	destinationSet, concurrencySet              bool
}

type runtime struct {
	flags  commandFlags
	config config.Config
	store  *queue.Store
	logger *observability.Logger
	client *http.Client
	errOut io.Writer
}

func parseFlags(name string, args []string, options Options) (runtime, []string, error) {
	flags := commandFlags{}
	set := commandFlagSet(name, &flags, options.Out)
	if err := set.Parse(args); err != nil {
		if err == flag.ErrHelp {
			flags.help = true
			return runtime{flags: flags}, nil, nil
		}
		return runtime{}, nil, err
	}
	if flags.help {
		set.Usage()
		return runtime{flags: flags}, set.Args(), nil
	}
	set.Visit(func(value *flag.Flag) {
		switch canonicalOption(value.Name) {
		case "name":
			flags.nameSet = true
		case "output":
			flags.outputSet = true
		case "config":
			flags.configSet = true
		case "state":
			flags.stateSet = true
		case "log":
			flags.logSet = true
		case "destination":
			flags.destinationSet = true
		case "concurrency":
			flags.concurrencySet = true
		case "timeout":
		case "ffmpeg":
		case "ffmpeg-path":
		case "webhook":
		}
	})
	workingDir := options.WorkingDir
	if workingDir == "" {
		var err error
		workingDir, err = os.Getwd()
		if err != nil {
			return runtime{}, nil, fmt.Errorf("resolve working directory: %w", err)
		}
	}
	if !filepath.IsAbs(workingDir) {
		return runtime{}, nil, fmt.Errorf("working directory must be absolute")
	}
	overrides := config.Overrides{}
	applyTransferOverrides(set, flags.transferFlags, &overrides)
	flags.jobOptions = downloader.Overrides{Retries: overrides.Retries, Resume: overrides.Resume, MaxHeight: overrides.MaxHeight, IdleTimeout: overrides.IdleTimeout}
	if visited(set, "ffmpeg") {
		flags.jobOptions.FFmpeg = &flags.ffmpeg
	}
	if flags.configSet {
		overrides.ConfigPath = flags.configPath
	}
	if flags.stateSet {
		overrides.StatePath = &flags.statePath
	}
	if flags.logSet {
		overrides.LogPath = &flags.logPath
	}
	if flags.destinationSet {
		overrides.Destination = &flags.destination
	}
	if flags.concurrencySet {
		overrides.Concurrency = &flags.concurrency
	}
	if visited(set, "timeout") {
		overrides.Timeout = &flags.timeout
	}
	if visited(set, "ffmpeg") {
		overrides.FFmpeg = &flags.ffmpeg
	}
	if visited(set, "ffmpeg-path") {
		overrides.FFmpegPath = &flags.ffmpegPath
	}
	if visited(set, "webhook") {
		overrides.WebhookURL = &flags.webhookURL
	}
	loaded, err := config.Load(config.LoadOptions{ConfigPath: flags.configPath, HomeDir: options.HomeDir, WorkingDir: workingDir, Env: options.Env, Overrides: overrides})
	if err != nil {
		return runtime{}, nil, err
	}
	if name == "list" || name == "status" {
		return runtime{flags: flags, config: loaded, errOut: options.ErrOut}, set.Args(), nil
	}
	if name == "add" {
		if err := os.MkdirAll(loaded.Destination, 0o755); err != nil {
			return runtime{}, nil, fmt.Errorf("create destination: %w", err)
		}
	}
	store, err := queue.NewStore(loaded.StatePath)
	if err != nil {
		return runtime{}, nil, err
	}
	logger, err := observability.New(observability.Options{LogDir: loaded.LogPath, WebhookURL: loaded.WebhookURL, NotifyCommand: loaded.NotifyCommand, Env: options.Env})
	if err != nil {
		return runtime{}, nil, err
	}
	client := options.Client
	if client == nil {
		client = downloader.NewHTTPClient(loaded.Timeout)
	}
	return runtime{flags: flags, config: loaded, store: store, logger: logger, client: client, errOut: options.ErrOut}, set.Args(), nil
}

func visited(set *flag.FlagSet, name string) bool {
	seen := false
	set.Visit(func(value *flag.Flag) {
		if canonicalOption(value.Name) == name {
			seen = true
		}
	})
	return seen
}

func (runtime runtime) close() error {
	if runtime.logger == nil {
		return nil
	}
	if err := runtime.logger.Close(); err != nil {
		if runtime.errOut != nil {
			_, _ = fmt.Fprintf(runtime.errOut, "videodl: close logger: %v\n", err)
		}
		return err
	}
	return nil
}

func (runtime runtime) emit(ctx context.Context, job queue.Job) {
	state := observability.StateProgress
	switch job.Status {
	case queue.StatusCompleted:
		state = observability.StateSucceeded
	case queue.StatusFailed:
		state = observability.StateFailed
	case queue.StatusCanceled:
		state = observability.StateCanceled
	case queue.StatusQueued, queue.StatusRunning:
	}
	event := observability.Event{JobID: job.ID, URL: job.URL, Name: job.Name, OutputPath: job.OutputPath, State: state, BytesDownloaded: job.BytesDownloaded, BytesTotal: job.BytesTotal}
	if job.Error != "" {
		event.Error = &observability.ErrorInfo{Message: job.Error}
	}
	_ = runtime.logger.Emit(ctx, event)
}
