package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"video-downloader/internal/config"
	"video-downloader/internal/downloader"
)

func runLegacy(ctx context.Context, options Options) error {
	flags := flag.NewFlagSet("videodl", flag.ContinueOnError)
	flags.SetOutput(options.ErrOut)
	var outputPath, ffmpegPath string
	var ffmpeg bool
	var timeout time.Duration
	var transfer transferFlags
	var configPath string
	bindTransferFlags(flags, &transfer)
	flags.StringVar(&configPath, "config", "", "fichier de configuration")
	flags.StringVar(&outputPath, "o", "", "chemin du fichier de sortie")
	flags.StringVar(&outputPath, "output", "", "chemin du fichier de sortie")
	flags.BoolVar(&ffmpeg, "ffmpeg", false, "forcer le traitement par ffmpeg")
	flags.StringVar(&ffmpegPath, "ffmpeg-path", "", "chemin de l'exécutable ffmpeg")
	flags.DurationVar(&timeout, "timeout", 30*time.Second, "délai réseau par connexion et réponse HTTP")
	if err := flags.Parse(options.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			legacyHelp(options.ErrOut)
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: videodl [flags] URL")
	}
	if outputPath == "" {
		return fmt.Errorf("le paramètre -o/--output est obligatoire")
	}
	request, err := downloader.NewRequest(flags.Arg(0), outputPath, ffmpeg)
	if err != nil {
		return err
	}
	workingDir, err := configWorkingDir(options)
	if err != nil {
		return err
	}
	overrides := config.Overrides{}
	applyTransferOverrides(flags, transfer, &overrides)
	if visited(flags, "ffmpeg") {
		overrides.FFmpeg = &ffmpeg
	}
	if visited(flags, "timeout") {
		overrides.Timeout = &timeout
	}
	if visited(flags, "ffmpeg-path") {
		overrides.FFmpegPath = &ffmpegPath
	}
	loaded, err := config.Load(config.LoadOptions{ConfigPath: configPath, HomeDir: options.HomeDir, WorkingDir: workingDir, Env: options.Env, Overrides: overrides})
	if err != nil {
		return err
	}
	request, err = downloader.NewRequest(flags.Arg(0), outputPath, loaded.FFmpeg)
	if err != nil {
		return err
	}
	client := options.Client
	if client == nil {
		client = downloader.NewHTTPClient(loaded.Timeout)
	}
	dl := downloader.New(client, loaded.FFmpegPath)
	dl.Configure(downloadSettings(loaded))
	if err := dl.Download(ctx, request); err != nil {
		return err
	}
	_, err = fmt.Fprintf(options.Out, "Téléchargement terminé: %s\n", request.OutputPath())
	return err
}
