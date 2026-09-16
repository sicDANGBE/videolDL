package downloader

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

func runFFmpeg(ctx context.Context, executable string, request Request, callback ProgressCallback) error {
	return runFFmpegInputs(ctx, executable, request, callback, "", time.Minute)
}
func runFFmpegInputs(ctx context.Context, executable string, request Request, callback ProgressCallback, audioURL string, idleTimeout time.Duration) (err error) {
	if callback != nil {
		if err := callback(Progress{}); err != nil {
			return fmt.Errorf("ffmpeg start progress: %w", err)
		}
	}
	outputPath := request.OutputPath()
	directory := filepath.Dir(outputPath)
	base := filepath.Base(outputPath)
	extension := filepath.Ext(base)
	temporary, err := os.CreateTemp(directory, "."+base+".*.part"+extension)
	if err != nil {
		return fmt.Errorf("create ffmpeg output: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if err != nil {
			if removeErr := os.Remove(temporaryPath); removeErr != nil && !os.IsNotExist(removeErr) {
				err = fmt.Errorf("%w; remove temporary ffmpeg output: %v", err, removeErr)
			}
		}
	}()
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close ffmpeg output: %w", err)
	}

	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	if idleTimeout > 0 {
		args = append(args, "-rw_timeout", strconv.FormatInt(idleTimeout.Microseconds(), 10))
	}
	args = append(args, "-i", request.Source().String())
	if audioURL != "" {
		if idleTimeout > 0 {
			args = append(args, "-rw_timeout", strconv.FormatInt(idleTimeout.Microseconds(), 10))
		}
		args = append(args, "-i", audioURL, "-map", "0:v:0?", "-map", "1:a:0")
	}
	args = append(args, "-c", "copy", temporaryPath)
	commandCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	monitorDone := make(chan struct{})
	monitorResult := make(chan error, 1)
	var size int64
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-monitorDone:
				monitorResult <- nil
				return
			case <-commandCtx.Done():
				monitorResult <- nil
				return
			case <-ticker.C:
				if err := request.space.external(temporaryPath, &size); err != nil {
					monitorResult <- err
					cancel()
					return
				}
			}
		}
	}()
	command := exec.CommandContext(commandCtx, executable, args...)
	output, runErr := command.CombinedOutput()
	close(monitorDone)
	spaceErr := <-monitorResult
	if spaceErr != nil {
		return spaceErr
	}
	if runErr != nil {
		return fmt.Errorf("ffmpeg: %w: %s", runErr, string(output))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := request.space.external(temporaryPath, &size); err != nil {
		return err
	}

	if err := publishOutput(temporaryPath, outputPath); err != nil {
		return fmt.Errorf("rename ffmpeg output: %w", err)
	}
	if callback != nil {
		info, statErr := os.Stat(outputPath)
		if statErr != nil {
			return fmt.Errorf("stat ffmpeg output: %w", statErr)
		}
		if err := callback(Progress{BytesDownloaded: info.Size(), TotalBytes: info.Size(), TotalBytesKnown: true}); err != nil {
			return fmt.Errorf("ffmpeg completion progress: %w", err)
		}
	}
	return nil
}
