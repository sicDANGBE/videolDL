package downloader

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"path"
	"strings"
	"time"
)

type Downloader struct {
	client     *http.Client
	ffmpegPath string
	settings   Settings
	space      *diskBudget
}

func New(client *http.Client, ffmpegPath string) *Downloader {
	if client == nil {
		client = http.DefaultClient
	}
	return &Downloader{client: client, ffmpegPath: ffmpegPath, settings: DefaultSettings(), space: newDiskBudget()}
}

func NewHTTPClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{Transport: transport}
}

func (d *Downloader) Download(ctx context.Context, request Request) (err error) {
	return d.DownloadWithProgress(ctx, request, nil)
}

func (d *Downloader) DownloadWithProgress(ctx context.Context, request Request, callback ProgressCallback) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	files, err := lockDestination(request.OutputPath())
	if err != nil {
		return err
	}
	defer files.close()
	reservation, err := d.space.reserve(request.OutputPath(), d.settings.MinFreeSpace)
	if err != nil {
		return err
	}
	defer reservation.release()
	request.space = reservation
	if (request.UseFFmpeg() || isDASH(request.Source())) && d.settings.MaxHeight > 0 {
		return fmt.Errorf("--max-height applies to native HLS selection; remove --ffmpeg and use an HLS master URL")
	}
	if request.UseFFmpeg() || isDASH(request.Source()) {
		return d.downloadWithFFmpeg(ctx, request, callback)
	}

	response, err := d.openFileResponse(ctx, request, files)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	if !isHLS(request.Source(), response.Header.Get("Content-Type")) {
		return d.downloadFile(ctx, request, response, callback, files)
	}
	defer response.Body.Close()

	parsed, err := parsePlaylist(response.Body)
	if err != nil {
		return fmt.Errorf("parse HLS source: %w", err)
	}
	return d.downloadPlaylist(ctx, playlistDownload{
		request:  request,
		source:   request.Source(),
		current:  parsed,
		callback: callback,
	})
}

type playlistDownload struct {
	request  Request
	source   URL
	current  playlist
	callback ProgressCallback
}

func (d *Downloader) downloadPlaylist(ctx context.Context, download playlistDownload) error {
	var audioURL string
	if len(download.current.variants) > 0 {
		selected, err := selectVariant(download.current.variants, d.settings.MaxHeight)
		if err != nil {
			return err
		}
		if audio := selectAudio(download.current.audio, selected.AudioGroup); audio != "" {
			resolved, err := resolveURL(download.source, audio)
			if err != nil {
				return err
			}
			audioURL = resolved.String()
		}
		variantURL, err := resolveURL(download.source, selected.URI)
		if err != nil {
			return fmt.Errorf("resolve HLS variant: %w", err)
		}
		download.current, err = d.fetchPlaylist(ctx, variantURL)
		if err != nil {
			return fmt.Errorf("fetch HLS variant: %w", err)
		}
		download.source = variantURL
	}
	if download.current.encrypted {
		return ErrEncryptedHLS
	}
	if len(download.current.segments) == 0 {
		return fmt.Errorf("HLS media playlist has no segments: %w", ErrInvalidPlaylist)
	}

	extension := strings.ToLower(path.Ext(download.request.OutputPath()))
	if audioURL != "" || download.current.requiresFFmpeg || (extension == ".mkv" || (download.current.mapURI == "" && extension == ".mp4")) {
		// Use FFmpeg for actual remuxing and separate audio, never rename TS bytes to MP4.
		selectedRequest, err := NewRequest(download.source.String(), download.request.OutputPath(), true)
		selectedRequest.space = download.request.space
		if err != nil {
			return err
		}
		executable := d.ffmpegPath
		if executable == "" {
			executable, err = exec.LookPath("ffmpeg")
			if err != nil {
				return ErrFFmpegMissing
			}
		}
		return runFFmpegInputs(ctx, executable, selectedRequest, download.callback, audioURL, d.settings.IdleTimeout)
	}
	return writeAtomic(download.request.OutputPath(), func(writer io.Writer) error {
		writer = download.request.space.writer(writer)
		state := &progressState{callback: download.callback}
		if download.current.mapURI != "" {
			mapURL, err := resolveURL(download.source, download.current.mapURI)
			if err != nil {
				return fmt.Errorf("resolve HLS initialization map: %w", err)
			}
			mapState := &progressState{}
			if err := d.copyURL(ctx, mapURL, writer, mapState); err != nil {
				return fmt.Errorf("download HLS initialization map: %w", err)
			}
			state.bytesDownloaded = mapState.bytesDownloaded
		}
		for index, item := range download.current.segments {
			segmentURL, err := resolveURL(download.source, item.URI)
			if err != nil {
				return fmt.Errorf("resolve HLS segment %d: %w", index+1, err)
			}
			state.segmentIndex = index + 1
			state.segmentTotal = len(download.current.segments)
			if err := d.copyURL(ctx, segmentURL, writer, state); err != nil {
				return fmt.Errorf("download HLS segment %d: %w", index+1, err)
			}
		}
		return nil
	})
}

func (d *Downloader) fetchPlaylist(ctx context.Context, source URL) (parsed playlist, err error) {
	response, err := d.get(ctx, source.String())
	if err != nil {
		return playlist{}, err
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close HLS playlist response: %w", closeErr)
		}
	}()
	parsed, err = parsePlaylist(response.Body)
	if err != nil {
		return playlist{}, err
	}
	return parsed, nil
}

func (d *Downloader) copyURL(ctx context.Context, source URL, destination io.Writer, state *progressState) (err error) {
	response, err := d.get(ctx, source.String())
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close media response: %w", closeErr)
		}
	}()
	if state == nil {
		if _, err := io.Copy(destination, response.Body); err != nil {
			return fmt.Errorf("copy response: %w", err)
		}
		return nil
	}
	if _, err := io.Copy(progressWriter{destination: destination, state: state}, response.Body); err != nil {
		return fmt.Errorf("copy response: %w", err)
	}
	return nil
}

func (d *Downloader) get(ctx context.Context, rawURL string) (*http.Response, error) {
	return d.fetch(ctx, rawURL, nil)
}

func resolveURL(base URL, reference string) (URL, error) {
	baseURL, err := url.Parse(base.String())
	if err != nil {
		return URL{}, fmt.Errorf("parse base URL: %w", err)
	}
	referenceURL, err := url.Parse(strings.TrimSpace(reference))
	if err != nil {
		return URL{}, fmt.Errorf("parse reference URL: %w", err)
	}
	return ParseURL(baseURL.ResolveReference(referenceURL).String())
}

func isHLS(source URL, contentType string) bool {
	parsed, err := url.Parse(source.String())
	if err == nil && strings.EqualFold(path.Ext(parsed.Path), ".m3u8") {
		return true
	}
	contentType = strings.ToLower(contentType)
	return strings.Contains(contentType, "mpegurl") || strings.Contains(contentType, "m3u8")
}

func isDASH(source URL) bool {
	parsed, err := url.Parse(source.String())
	return err == nil && strings.EqualFold(path.Ext(parsed.Path), ".mpd")
}

func (d *Downloader) downloadWithFFmpeg(ctx context.Context, request Request, callback ProgressCallback) error {
	ffmpeg := d.ffmpegPath
	if ffmpeg == "" {
		var err error
		ffmpeg, err = exec.LookPath("ffmpeg")
		if err != nil {
			return fmt.Errorf("%w: install ffmpeg or pass --ffmpeg-path", ErrFFmpegMissing)
		}
	}
	return runFFmpegInputs(ctx, ffmpeg, request, callback, "", d.settings.IdleTimeout)
}
