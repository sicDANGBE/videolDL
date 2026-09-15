package observability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Options struct {
	LogDir         string
	WebhookURL     string
	NotifyCommand  string
	WebhookTimeout time.Duration
	Terminal       io.Writer
	Env            []string
}

type Logger struct {
	mu       sync.Mutex
	events   *os.File
	success  *os.File
	errors   *os.File
	webhooks *os.File
	notifies *os.File
	webhook  *Webhook
	notify   *notifyCommand
	terminal *Terminal
}

func New(options Options) (*Logger, error) {
	if options.LogDir == "" {
		return nil, fmt.Errorf("log directory is empty: %w", ErrInvalidEvent)
	}
	if err := os.MkdirAll(options.LogDir, 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	events, err := os.OpenFile(filepath.Join(options.LogDir, "events.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open event log: %w", err)
	}
	success, err := os.OpenFile(filepath.Join(options.LogDir, "success.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		_ = events.Close()
		return nil, fmt.Errorf("open success log: %w", err)
	}
	errorsFile, err := os.OpenFile(filepath.Join(options.LogDir, "error.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		_ = success.Close()
		_ = events.Close()
		return nil, fmt.Errorf("open error log: %w", err)
	}
	webhook, err := newWebhook(options.WebhookURL, options.WebhookTimeout)
	if err != nil {
		_ = events.Close()
		_ = success.Close()
		_ = errorsFile.Close()
		return nil, err
	}
	notify, err := newNotifyCommand(options.NotifyCommand, options.Env)
	if err != nil {
		_ = events.Close()
		_ = success.Close()
		_ = errorsFile.Close()
		return nil, err
	}
	return &Logger{events: events, success: success, errors: errorsFile, webhook: webhook, notify: notify, terminal: terminalFor(options.Terminal)}, nil
}

func terminalFor(out io.Writer) *Terminal {
	if out == nil {
		return nil
	}
	return NewTerminal(out)
}

func (logger *Logger) Emit(ctx context.Context, event Event) error {
	if err := event.valid(); err != nil {
		return err
	}
	safe := event.safe()
	logger.mu.Lock()
	defer logger.mu.Unlock()
	if err := writeJSONLine(logger.events, safe); err != nil {
		return fmt.Errorf("write event log: %w", err)
	}
	switch safe.State {
	case StateSucceeded:
		if err := writeJSONLine(logger.success, safe); err != nil {
			return fmt.Errorf("write success log: %w", err)
		}
	case StateFailed:
		if err := writeJSONLine(logger.errors, safe); err != nil {
			return fmt.Errorf("write error log: %w", err)
		}
	}
	if logger.webhook != nil {
		if err := logger.webhook.deliver(ctx, safe); err != nil {
			if logErr := logger.writeWebhookError(safe, err); logErr != nil {
				return fmt.Errorf("write webhook error log: %w", logErr)
			}
		}
	}
	if logger.notify != nil && (safe.State == StateSucceeded || safe.State == StateFailed) {
		if err := logger.notify.Deliver(ctx, safe); err != nil {
			if logErr := logger.writeNotifyError(safe, err); logErr != nil {
				return fmt.Errorf("write notify error log: %w", logErr)
			}
		}
	}
	if logger.terminal != nil {
		if err := logger.terminal.Write(safe); err != nil {
			return fmt.Errorf("write terminal event: %w", err)
		}
	}
	return nil
}

func writeJSONLine(writer io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "%s\n", data)
	return err
}

type webhookError struct {
	Event      Event     `json:"event"`
	Error      string    `json:"error"`
	RecordedAt time.Time `json:"recorded_at"`
}

type notifyError struct {
	Event      Event     `json:"event"`
	Error      string    `json:"error"`
	RecordedAt time.Time `json:"recorded_at"`
}

func (logger *Logger) writeWebhookError(event Event, deliveryErr error) error {
	if logger.webhooks == nil {
		file, err := os.OpenFile(filepath.Join(filepath.Dir(logger.events.Name()), "webhook-errors.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		logger.webhooks = file
	}
	return writeJSONLine(logger.webhooks, webhookError{Event: event.safe(), Error: redactMessage(deliveryErr.Error()), RecordedAt: time.Now().UTC()})
}

func (logger *Logger) writeNotifyError(event Event, deliveryErr error) error {
	if logger.notifies == nil {
		file, err := os.OpenFile(filepath.Join(filepath.Dir(logger.events.Name()), "notify-errors.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		logger.notifies = file
	}
	return writeJSONLine(logger.notifies, notifyError{Event: event.safe(), Error: redactMessage(deliveryErr.Error()), RecordedAt: time.Now().UTC()})
}

func (logger *Logger) Close() error {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	var closeErrs []error
	if logger.webhooks != nil {
		if err := logger.webhooks.Close(); err != nil {
			closeErrs = append(closeErrs, fmt.Errorf("close webhook log: %w", err))
		}
	}
	if logger.notifies != nil {
		if err := logger.notifies.Close(); err != nil {
			closeErrs = append(closeErrs, fmt.Errorf("close notify log: %w", err))
		}
	}
	if err := logger.events.Close(); err != nil {
		closeErrs = append(closeErrs, fmt.Errorf("close event log: %w", err))
	}
	if err := logger.success.Close(); err != nil {
		closeErrs = append(closeErrs, fmt.Errorf("close success log: %w", err))
	}
	if err := logger.errors.Close(); err != nil {
		closeErrs = append(closeErrs, fmt.Errorf("close error log: %w", err))
	}
	return errors.Join(closeErrs...)
}
