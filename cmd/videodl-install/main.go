package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"video-downloader/internal/installer"
)

func main() {
	options := installer.Options{Out: os.Stdout}
	flag.StringVar(&options.Prefix, "prefix", "", "préfixe d’installation absolu")
	flag.StringVar(&options.Binary, "binary", "", "exécutable compilé")
	flag.StringVar(&options.Manual, "manual", "", "manuel préparé")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "arguments inattendus")
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()
	if err := installer.Install(ctx, options); err != nil {
		fmt.Fprintln(os.Stderr, "Installation interrompue :", err)
		os.Exit(1)
	}
}
