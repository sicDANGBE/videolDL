package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type downloadFiles struct {
	part, metadata string
	lock           *os.File
}
type resumeState struct {
	SourceHash string `json:"source_hash"`
	ETag       string `json:"etag"`
	Total      int64  `json:"total"`
}

func sourceHash(source string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(source))) }
func lockDestination(output string) (*downloadFiles, error) {
	if _, err := os.Lstat(output); err == nil {
		return nil, ErrOutputExists
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	directory := filepath.Join(filepath.Dir(output), ".videodl")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("resume directory must be a real directory")
	}
	base := filepath.Join(directory, sourceHash(filepath.Base(output)))
	lock, err := openRegular(base+".lock", os.O_CREATE|os.O_RDWR)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, ErrDownloadBusy
	}
	// The lock file stays in place so all processes lock the same inode.
	return &downloadFiles{part: base + ".part", metadata: base + ".json", lock: lock}, nil
}
func openRegular(path string, flags int) (*os.File, error) {
	fd, err := syscall.Open(path, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("download state must be a regular file")
	}
	return file, nil
}
func (f *downloadFiles) close() { syscall.Flock(int(f.lock.Fd()), syscall.LOCK_UN); f.lock.Close() }
func (f *downloadFiles) clear() { _ = os.Remove(f.part); _ = os.Remove(f.metadata) }
func (f *downloadFiles) load(source string) (resumeState, int64) {
	var state resumeState
	data, err := os.ReadFile(f.metadata)
	if err != nil || json.Unmarshal(data, &state) != nil || state.SourceHash != sourceHash(source) || !strongETag(state.ETag) {
		return resumeState{}, 0
	}
	info, err := os.Lstat(f.part)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || (state.Total >= 0 && info.Size() >= state.Total) {
		return resumeState{}, 0
	}
	return state, info.Size()
}
func strongETag(value string) bool {
	return strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") && len(value) >= 2
}
func (f *downloadFiles) save(state resumeState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(f.metadata), ".resume-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), f.metadata)
}

func (d *Downloader) downloadFile(ctx context.Context, request Request, response *http.Response, callback ProgressCallback, files *downloadFiles) error {
	for attempt := 0; ; attempt++ {
		err := d.fileAttempt(ctx, request, response, callback, files)
		response = nil
		if err == nil {
			files.clear()
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt >= d.settings.Retries || !retryable(err) {
			return err
		}
		if err := retryPause(ctx, attempt, ""); err != nil {
			return err
		}
	}
}
func (d *Downloader) fileAttempt(ctx context.Context, request Request, response *http.Response, callback ProgressCallback, files *downloadFiles) (err error) {
	if response == nil {
		response, err = d.openFileResponse(ctx, request, files)
		if err != nil {
			return err
		}
	}
	state, offset := resumeState{}, int64(0)
	if d.settings.Resume {
		state, offset = files.load(request.Source().String())
	}
	defer response.Body.Close()
	if strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/html") {
		return fmt.Errorf("source returned a web page; provide a direct media URL")
	}
	if response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
		return fmt.Errorf("unsupported content encoding for a resumable download")
	}
	total := response.ContentLength
	if response.StatusCode == http.StatusPartialContent {
		var start, end, full int64
		count, scanErr := fmt.Sscanf(response.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &full)
		if offset == 0 || scanErr != nil || count != 3 || start != offset || end < start || full <= end || end != full-1 || (response.ContentLength >= 0 && response.ContentLength != end-start+1) || (state.Total >= 0 && state.Total != full) || response.Header.Get("ETag") != state.ETag {
			files.clear()
			return fmt.Errorf("invalid resume response; partial data discarded, retry from the beginning")
		}
		total = full
	} else if response.StatusCode == http.StatusOK {
		offset = 0
	} else {
		return fmt.Errorf("unexpected media HTTP status %d", response.StatusCode)
	}
	state = resumeState{SourceHash: sourceHash(request.Source().String()), ETag: response.Header.Get("ETag"), Total: total}
	resumable := d.settings.Resume && strongETag(state.ETag)
	if !resumable {
		defer files.clear()
	}
	flags := os.O_CREATE | os.O_WRONLY
	if offset == 0 {
		flags |= os.O_TRUNC
	}
	file, err := openRegular(files.part, flags)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	if resumable {
		if err := files.save(state); err != nil {
			return err
		}
	} else {
		_ = os.Remove(files.metadata)
	}
	progress := &progressState{callback: callback, bytesDownloaded: offset, totalBytes: max(total, 0), totalKnown: total >= 0}
	if _, err := io.Copy(progressWriter{destination: file, state: progress}, response.Body); err != nil {
		return fmt.Errorf("copy media: %w", err)
	}
	if total >= 0 && progress.bytesDownloaded != total {
		return io.ErrUnexpectedEOF
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		return err
	}
	return publishOutput(files.part, request.OutputPath())
}

func (d *Downloader) openFileResponse(ctx context.Context, request Request, files *downloadFiles) (*http.Response, error) {
	headers := http.Header{}
	offset := int64(0)
	if d.settings.Resume {
		state, size := files.load(request.Source().String())
		offset = size
		if offset > 0 {
			headers.Set("Range", fmt.Sprintf("bytes=%d-", offset))
			headers.Set("If-Range", state.ETag)
		}
	}
	response, err := d.fetch(ctx, request.Source().String(), headers)
	var status *HTTPError
	if offset > 0 && errors.As(err, &status) && status.Status == http.StatusRequestedRangeNotSatisfiable {
		files.clear()
		return d.fetch(ctx, request.Source().String(), nil)
	}
	return response, err
}
