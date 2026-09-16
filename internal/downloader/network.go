package downloader

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Settings are immutable while a Downloader is in use.
type Settings struct {
	MinFreeSpace int64
	Retries      int
	Resume       bool
	MaxHeight    int
	IdleTimeout  time.Duration
}

func DefaultSettings() Settings {
	return Settings{MinFreeSpace: 2 << 30, Retries: 3, Resume: true, IdleTimeout: 60 * time.Second}
}
func (d *Downloader) Configure(settings Settings) { d.settings = settings }

func retryable(err error) bool {
	var status *HTTPError
	if errors.As(err, &status) {
		switch status.Status {
		case 408, 429, 500, 502, 503, 504:
			return true
		}
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var network net.Error
	return errors.As(err, &network) || errors.Is(err, io.ErrUnexpectedEOF)
}

func retryPause(ctx context.Context, attempt int, after string) error {
	delay := time.Duration(1<<min(attempt, 5)) * 250 * time.Millisecond
	if n, err := strconv.Atoi(after); err == nil && n >= 0 {
		delay = max(delay, time.Duration(min(n, 300))*time.Second)
	} else if date, err := http.ParseTime(after); err == nil {
		delay = max(delay, min(time.Until(date), 5*time.Minute))
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (d *Downloader) fetch(ctx context.Context, rawURL string, headers http.Header) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		requestContext, cancel := context.WithCancel(ctx)
		req, err := http.NewRequestWithContext(requestContext, http.MethodGet, rawURL, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		req.Header.Set("Accept-Encoding", "identity")
		for key, values := range headers {
			req.Header[key] = values
		}
		response, err := d.client.Do(req)
		after := ""
		if err == nil && (response.StatusCode < 200 || response.StatusCode >= 300) {
			after = response.Header.Get("Retry-After")
			err = &HTTPError{URL: rawURL, Status: response.StatusCode}
			response.Body.Close()
		}
		if err == nil {
			response.Body = newIdleBody(response.Body, cancel, d.settings.IdleTimeout)
			return response, nil
		}
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if attempt >= d.settings.Retries || !retryable(err) {
			return nil, err
		}
		if err := retryPause(ctx, attempt, after); err != nil {
			return nil, err
		}
	}
}

type idleBody struct {
	io.ReadCloser
	cancel     context.CancelFunc
	timeout    time.Duration
	timer      *time.Timer
	mu         sync.Mutex
	expired    bool
	reading    bool
	generation uint64
}
type idleError struct{}

func (idleError) Error() string   { return "download stalled: no data received before idle timeout" }
func (idleError) Timeout() bool   { return true }
func (idleError) Temporary() bool { return true }
func newIdleBody(body io.ReadCloser, cancel context.CancelFunc, timeout time.Duration) *idleBody {
	b := &idleBody{ReadCloser: body, cancel: cancel, timeout: timeout}
	// Measure blocked reads, not time spent writing progress or waiting on disk.
	return b
}
func (b *idleBody) Read(p []byte) (int, error) {
	if b.timeout <= 0 {
		return b.ReadCloser.Read(p)
	}
	b.mu.Lock()
	b.reading = true
	b.generation++
	generation := b.generation
	b.timer = time.AfterFunc(b.timeout, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.reading && b.generation == generation {
			b.expired = true
			b.cancel()
		}
	})
	b.mu.Unlock()
	n, err := b.ReadCloser.Read(p)
	b.mu.Lock()
	b.timer.Stop()
	b.reading = false
	expired := b.expired
	b.mu.Unlock()
	if expired {
		return n, idleError{}
	}
	return n, err
}
func (b *idleBody) Close() error { b.cancel(); return b.ReadCloser.Close() }

// Overrides persist only explicitly supplied per-job transfer settings.
type Overrides struct {
	Retries     *int           `json:"retries,omitempty"`
	Resume      *bool          `json:"resume,omitempty"`
	MaxHeight   *int           `json:"max_height,omitempty"`
	IdleTimeout *time.Duration `json:"idle_timeout,omitempty"`
	FFmpeg      *bool          `json:"ffmpeg,omitempty"`
}

func (s *Settings) Apply(o Overrides) {
	if o.Retries != nil {
		s.Retries = *o.Retries
	}
	if o.Resume != nil {
		s.Resume = *o.Resume
	}
	if o.MaxHeight != nil {
		s.MaxHeight = *o.MaxHeight
	}
	if o.IdleTimeout != nil {
		s.IdleTimeout = *o.IdleTimeout
	}
}

func (d *Downloader) Settings() Settings { return d.settings }
