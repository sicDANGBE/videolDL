package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"video-downloader/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	options := cli.Options{Args: os.Args[1:], Out: os.Stdout, ErrOut: os.Stderr}
	if err := cli.Run(ctx, options); err != nil {
		slog.Error("download failed", slog.Any("err", err))
		os.Exit(1)
	}
}
