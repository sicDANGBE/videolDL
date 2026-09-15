package downloader

import (
	"errors"
	"fmt"
)

var (
	ErrOutputExists    = errors.New("destination already exists; choose a different output name")
	ErrDownloadBusy    = errors.New("another download is using this destination")
	ErrInvalidURL      = errors.New("invalid video URL")
	ErrInvalidOutput   = errors.New("invalid output path")
	ErrInvalidPlaylist = errors.New("invalid HLS playlist")
	ErrEncryptedHLS    = errors.New("encrypted HLS is not handled by the built-in downloader")
	ErrFFmpegMissing   = errors.New("ffmpeg is not available")
)

type HTTPError struct {
	URL    string
	Status int
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d while fetching %s", e.Status, e.URL)
}
