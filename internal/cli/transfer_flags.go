package cli

import (
	"flag"
	"time"
	"video-downloader/internal/config"
	"video-downloader/internal/downloader"
)

type transferFlags struct {
	retries, maxHeight int
	resume             bool
	idleTimeout        time.Duration
}

func applyTransferOverrides(set *flag.FlagSet, f transferFlags, o *config.Overrides) {
	if visited(set, "retries") {
		o.Retries = &f.retries
	}
	if visited(set, "resume") {
		o.Resume = &f.resume
	}
	if visited(set, "max-height") {
		o.MaxHeight = &f.maxHeight
	}
	if visited(set, "idle-timeout") {
		o.IdleTimeout = &f.idleTimeout
	}
}
func downloadSettings(c config.Config) downloader.Settings {
	return downloader.Settings{MinFreeSpace: c.MinFreeSpace, Retries: c.Retries, Resume: c.Resume, MaxHeight: c.MaxHeight, IdleTimeout: c.IdleTimeout}
}
