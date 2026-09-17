package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"video-downloader/internal/config"
	"video-downloader/internal/queue"
)

type watchFlags struct {
	configPath string
	statePath  string
	interval   time.Duration
	json       bool
	once       bool
	help       bool
	configSet  bool
	stateSet   bool
}

type watchReport struct {
	Daemon       watchDaemon `json:"daemon"`
	Totals       watchTotals `json:"totals"`
	ActiveCount  int         `json:"active_count"`
	RunningCount int         `json:"running_count"`
	Jobs         []queue.Job `json:"jobs"`
	Recent       []queue.Job `json:"recent_events"`
}

type watchRuntime struct {
	out    io.Writer
	store  *queue.Store
	config config.Config
}

type watchDaemon struct {
	Status  string `json:"status"`
	PID     int    `json:"pid,omitempty"`
	Message string `json:"message,omitempty"`
}

type watchTotals struct {
	Queued    int `json:"queued"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Canceled  int `json:"canceled"`
}

func runWatch(ctx context.Context, options Options, args []string) error {
	flags, positional, err := parseWatchFlags(args, options.Out)
	if err != nil {
		return err
	}
	if flags.help {
		return nil
	}
	if len(positional) != 0 {
		return fmt.Errorf("usage: videodl watch [flags]")
	}
	loaded, err := loadWatchConfig(options, flags)
	if err != nil {
		return err
	}
	store, err := queue.NewStore(loaded.StatePath)
	if err != nil {
		return err
	}
	return watchQueue(ctx, watchRuntime{out: options.Out, store: store, config: loaded}, flags)
}

func parseWatchFlags(args []string, out io.Writer) (watchFlags, []string, error) {
	values := commandFlags{}
	set := commandFlagSet("watch", &values, out)
	flags := watchFlags{}
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.help = true
			return flags, nil, nil
		}
		return watchFlags{}, nil, err
	}
	flags = watchFlags{configPath: values.configPath, statePath: values.statePath, json: values.json, once: values.once, interval: values.interval, help: values.help}
	set.Visit(func(value *flag.Flag) {
		switch canonicalOption(value.Name) {
		case "config":
			flags.configSet = true
		case "state":
			flags.stateSet = true
		}
	})
	if flags.help {
		set.Usage()
	}
	if flags.interval <= 0 {
		return watchFlags{}, nil, fmt.Errorf("interval must be positive")
	}
	return flags, set.Args(), nil
}

func loadWatchConfig(options Options, flags watchFlags) (config.Config, error) {
	workingDir, err := configWorkingDir(options)
	if err != nil {
		return config.Config{}, err
	}
	overrides := config.Overrides{}
	if flags.configSet {
		overrides.ConfigPath = flags.configPath
	}
	if flags.stateSet {
		overrides.StatePath = &flags.statePath
	}
	return config.Load(config.LoadOptions{ConfigPath: flags.configPath, HomeDir: options.HomeDir, WorkingDir: workingDir, Env: options.Env, Overrides: overrides})
}

func watchQueue(ctx context.Context, runtime watchRuntime, flags watchFlags) error {
	if err := renderWatch(ctx, runtime, flags.json); err != nil || flags.once {
		return err
	}
	ticker := time.NewTicker(flags.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := renderWatch(ctx, runtime, flags.json); err != nil {
				return err
			}
		}
	}
}

func renderWatch(ctx context.Context, runtime watchRuntime, jsonOutput bool) error {
	jobs, err := queue.Snapshot(runtime.config.StatePath)
	if err != nil {
		return err
	}
	report := newWatchReport(jobs, inspectDaemon(runtime.config.DaemonPIDPath))
	if jsonOutput {
		return json.NewEncoder(runtime.out).Encode(report)
	}
	_, err = fmt.Fprint(runtime.out, formatWatch(report))
	return err
}

func newWatchReport(jobs []queue.Job, daemon daemonState) watchReport {
	report := watchReport{Daemon: watchDaemon{Status: daemon.status, PID: daemon.pid, Message: daemon.message}, Jobs: append([]queue.Job(nil), jobs...)}
	for _, job := range jobs {
		switch job.Status {
		case queue.StatusQueued:
			report.Totals.Queued++
			report.ActiveCount++
		case queue.StatusRunning:
			report.Totals.Running++
			report.ActiveCount++
			report.RunningCount++
		case queue.StatusCompleted:
			report.Totals.Completed++
			report.Recent = append(report.Recent, job)
		case queue.StatusFailed:
			report.Totals.Failed++
			report.Recent = append(report.Recent, job)
		case queue.StatusCanceled:
			report.Totals.Canceled++
		}
	}
	sort.SliceStable(report.Recent, func(i, j int) bool { return report.Recent[i].UpdatedAt.Before(report.Recent[j].UpdatedAt) })
	if len(report.Recent) > 5 {
		report.Recent = append([]queue.Job(nil), report.Recent[len(report.Recent)-5:]...)
	}
	return report
}

func formatWatch(report watchReport) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "daemon=%s", report.Daemon.Status)
	if report.Daemon.PID != 0 {
		fmt.Fprintf(&builder, " pid=%d", report.Daemon.PID)
	}
	if report.Daemon.Message != "" {
		fmt.Fprintf(&builder, " message=%s", report.Daemon.Message)
	}
	fmt.Fprintf(&builder, " active=%d running=%d queued=%d completed=%d failed=%d canceled=%d\n", report.ActiveCount, report.RunningCount, report.Totals.Queued, report.Totals.Completed, report.Totals.Failed, report.Totals.Canceled)
	builder.WriteString("jobs:\n")
	for _, job := range report.Jobs {
		fmt.Fprintf(&builder, "%s %s %s", job.ID, job.Status, job.Name)
		if job.Error != "" {
			fmt.Fprintf(&builder, " error=%s", job.Error)
		}
		builder.WriteByte('\n')
	}
	builder.WriteString("recent_events:\n")
	for _, job := range report.Recent {
		fmt.Fprintf(&builder, "%s %s %s", job.ID, job.Status, job.Name)
		if job.Error != "" {
			fmt.Fprintf(&builder, " error=%s", job.Error)
		}
		builder.WriteByte('\n')
	}
	return builder.String()
}
