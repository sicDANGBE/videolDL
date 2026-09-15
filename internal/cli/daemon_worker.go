package cli

import (
	"context"
	"fmt"

	"video-downloader/internal/downloader"
	"video-downloader/internal/worker"
)

func runDaemonWorker(ctx context.Context, options Options, args []string) error {
	runtime, positional, err := parseFlags("__daemon-worker", args, options)
	if err != nil {
		return err
	}
	if runtime.flags.help {
		return nil
	}
	defer runtime.close()
	if len(positional) != 0 {
		return fmt.Errorf("usage: videodl __daemon-worker [flags]")
	}
	dl := downloader.New(runtime.client, runtime.config.FFmpegPath)
	dl.Configure(downloadSettings(runtime.config))
	workerDownloader := &configuredDownloader{delegate: dl, ffmpeg: runtime.config.FFmpeg}
	service, err := worker.New(runtime.store, workerDownloader, worker.Options{Concurrency: runtime.config.Concurrency})
	if err != nil {
		return err
	}
	return service.Watch(ctx)
}
