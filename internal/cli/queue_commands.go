package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"video-downloader/internal/downloader"
	"video-downloader/internal/queue"
	"video-downloader/internal/worker"
)

func runAdd(ctx context.Context, options Options, args []string) error {
	rt, positional, err := parseAddFlags(args, options)
	if err != nil {
		return err
	}
	if rt.flags.help {
		return nil
	}
	defer rt.close()
	name, rawURL, err := addArguments(rt.flags, positional)
	if err != nil {
		return err
	}
	source, err := downloader.ParseURL(rawURL)
	if err != nil {
		return err
	}
	if name == "" {
		name = filepath.Base(sourcePath(source.String()))
	}
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "video.bin"
	}
	if rt.flags.json {
		name = strings.TrimSpace(name)
	}
	outputPath, err := safeOutput(rt.config.Destination, name)
	if err != nil {
		return err
	}
	job, err := rt.store.Add(ctx, queue.AddInput{DownloadOptions: rt.flags.jobOptions, URL: source.String(), Name: name, OutputPath: outputPath})
	if err != nil {
		return err
	}
	rt.emit(ctx, job)
	if rt.config.AutoStartWorker {
		flags := daemonFlags{configPath: rt.flags.configPath}
		if err := daemonStartWhenStopped(ctx, options, flags, rt.config); err != nil {
			return err
		}
	}
	return writeValue(options, rt.flags.json, job, fmt.Sprintf("added %s -> %s\n", job.ID, job.OutputPath))
}

func addArguments(flags commandFlags, args []string) (string, string, error) {
	if flags.nameSet && flags.outputSet {
		return "", "", fmt.Errorf("nom défini deux fois : utiliser --name/-n ou --output/-o")
	}
	name := flags.name
	if flags.outputSet {
		name = flags.output
	}
	if (flags.nameSet || flags.outputSet) && name == "" {
		return "", "", fmt.Errorf("le nom du fichier ne doit pas être vide")
	}
	switch len(args) {
	case 1:
		return name, args[0], nil
	case 2:
		if flags.nameSet || flags.outputSet {
			return "", "", fmt.Errorf("nom défini deux fois : utiliser add NOM URL ou add --name NOM URL")
		}
		if args[0] == "" {
			return "", "", fmt.Errorf("le nom du fichier ne doit pas être vide")
		}
		return args[0], args[1], nil
	default:
		return "", "", fmt.Errorf("usage: videodl add [options] [NOM] URL")
	}
}

func parseAddFlags(args []string, options Options) (runtime, []string, error) {
	rt, positional, err := parseFlags("add", args, options)
	if err != nil {
		return runtime{}, nil, err
	}
	return rt, positional, nil
}

func runWorker(ctx context.Context, options Options, args []string) error {
	runtime, positional, err := parseFlags("worker", args, options)
	if err != nil {
		return err
	}
	if runtime.flags.help {
		return nil
	}
	defer runtime.close()
	if len(positional) != 0 {
		return fmt.Errorf("usage: videodl worker [flags]")
	}
	release, err := runtime.store.LockSupervisor()
	if err != nil {
		return err
	}
	defer release()
	dl := downloader.New(runtime.client, runtime.config.FFmpegPath)
	dl.Configure(downloadSettings(runtime.config))
	workerDownloader := &configuredDownloader{delegate: dl, ffmpeg: runtime.config.FFmpeg, overrides: runtime.flags.jobOptions}
	service, err := worker.New(runtime.store, workerDownloader, worker.Options{Concurrency: runtime.config.Concurrency})
	if err != nil {
		return err
	}
	if runtime.flags.watch {
		return service.Watch(ctx)
	}
	err = service.Run(ctx)
	jobs, listErr := runtime.store.List(ctx)
	if listErr == nil {
		for _, job := range jobs {
			runtime.emit(ctx, job)
		}
	}
	if err != nil {
		return err
	}
	return listErr
}

type configuredDownloader struct {
	delegate  *downloader.Downloader
	ffmpeg    bool
	overrides downloader.Overrides
}

func (d *configuredDownloader) DownloadWithProgress(ctx context.Context, request downloader.Request, callback downloader.ProgressCallback) error {
	delegate := *d.delegate
	settings := delegate.Settings()
	settings.Apply(request.Options())
	settings.Apply(d.overrides)
	delegate.Configure(settings)
	ffmpeg := d.ffmpeg
	if request.Options().FFmpeg != nil {
		ffmpeg = *request.Options().FFmpeg
	}
	if d.overrides.FFmpeg != nil {
		ffmpeg = *d.overrides.FFmpeg
	}
	if ffmpeg {
		var err error
		request, err = downloader.NewRequest(request.Source().String(), request.OutputPath(), true)
		if err != nil {
			return err
		}
	}
	return delegate.DownloadWithProgress(ctx, request, callback)
}

func runList(ctx context.Context, options Options, args []string) error {
	runtime, positional, err := parseFlags("list", args, options)
	if err != nil {
		return err
	}
	if runtime.flags.help {
		return nil
	}
	defer runtime.close()
	if len(positional) != 0 {
		return fmt.Errorf("usage: videodl list [flags]")
	}
	jobs, err := queue.Snapshot(runtime.config.StatePath)
	if err != nil {
		return err
	}
	return writeValue(options, runtime.flags.json, jobs, formatJobs(jobs))
}

func runStatus(ctx context.Context, options Options, args []string) error {
	runtime, positional, err := parseFlags("status", args, options)
	if err != nil {
		return err
	}
	if runtime.flags.help {
		return nil
	}
	defer runtime.close()
	if len(positional) != 1 {
		return fmt.Errorf("usage: videodl status [flags] JOB_ID")
	}
	jobs, err := queue.Snapshot(runtime.config.StatePath)
	var job queue.Job
	if err == nil {
		err = queue.ErrNotFound
		for _, candidate := range jobs {
			if candidate.ID == positional[0] {
				job = candidate
				err = nil
				break
			}
		}
	}
	if err != nil {
		return err
	}
	return writeValue(options, runtime.flags.json, job, fmt.Sprintf("%s — %s\nFichier : %s\nProgression : %d octets / %d\nErreur : %s\n", job.ID, job.Status, job.OutputPath, job.BytesDownloaded, job.BytesTotal, job.Error))
}

func runTransition(ctx context.Context, options Options, args []string, retry bool) error {
	name := "cancel"
	if retry {
		name = "retry"
	}
	runtime, positional, err := parseFlags(name, args, options)
	if err != nil {
		return err
	}
	if runtime.flags.help {
		return nil
	}
	defer runtime.close()
	if len(positional) != 1 {
		return fmt.Errorf("usage: videodl %s [flags] JOB_ID", name)
	}
	if retry {
		err = runtime.store.Retry(ctx, positional[0])
	} else {
		err = runtime.store.Cancel(ctx, positional[0])
	}
	if err != nil {
		return err
	}
	job, err := runtime.store.Get(ctx, positional[0])
	if err != nil {
		return err
	}
	runtime.emit(ctx, job)
	return writeValue(options, runtime.flags.json, job, fmt.Sprintf("%s %s\n", job.ID, job.Status))
}

func writeValue(options Options, jsonOutput bool, value any, text string) error {
	if jsonOutput {
		return json.NewEncoder(options.Out).Encode(value)
	}
	_, err := fmt.Fprint(options.Out, text)
	return err
}

func safeOutput(destination, name string) (string, error) {
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) || name == "." || name == ".." {
		return "", fmt.Errorf("output name must be a safe file name")
	}
	path := filepath.Join(destination, name)
	relative, err := filepath.Rel(destination, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("output path escapes destination")
	}
	return path, nil
}

func sourcePath(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Path
}

func formatJobs(jobs []queue.Job) string {
	if len(jobs) == 0 {
		return "File vide. Ajoutez une URL avec : videodl add --name video.mp4 URL\n"
	}
	var builder strings.Builder
	writer := tabwriter.NewWriter(&builder, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "IDENTIFIANT\tÉTAT\tOCTETS\tFICHIER")
	for _, job := range jobs {
		fmt.Fprintf(writer, "%s\t%s\t%d\t%s\n", job.ID, job.Status, job.BytesDownloaded, job.Name)
	}
	writer.Flush()
	return builder.String()
}
