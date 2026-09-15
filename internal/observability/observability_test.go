package observability

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testEvent(state State) Event {
	event := Event{
		JobID:           "job-7",
		URL:             "https://example.test/video?token=secret#private",
		Name:            "clip.mp4",
		OutputPath:      "/tmp/clip.mp4",
		State:           state,
		StartedAt:       time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC),
		FinishedAt:      time.Date(2026, 9, 9, 12, 0, 1, 0, time.UTC),
		BytesDownloaded: 12,
		BytesTotal:      20,
	}
	if state == StateFailed {
		event.Error = &ErrorInfo{Message: "download failed"}
	}
	return event
}

func TestLogger_Emit_appends_valid_jsonl_when_called_concurrently(t *testing.T) {
	// Given
	directory := t.TempDir()
	logger, err := New(Options{LogDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	// When
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if emitErr := logger.Emit(context.Background(), testEvent(StateSucceeded)); emitErr != nil {
				t.Errorf("emit event: %v", emitErr)
			}
		}()
	}
	group.Wait()

	// Then
	file, err := os.Open(filepath.Join(directory, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("invalid JSONL record %q: %v", scanner.Text(), err)
		}
		if event.URL != "https://example.test/video" {
			t.Errorf("URL leaked query or fragment: %q", event.URL)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 20 {
		t.Fatalf("got %d records, want 20", count)
	}
}

func TestLogger_Emit_rejects_malformed_event_without_writing(t *testing.T) {
	// Given
	directory := t.TempDir()
	logger, err := New(Options{LogDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	// When
	err = logger.Emit(context.Background(), Event{State: StateSucceeded})

	// Then
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("got %v, want ErrInvalidEvent", err)
	}
}

func TestTerminal_Write_is_deterministic_and_does_not_echo_url_or_secret(t *testing.T) {
	// Given
	var output strings.Builder
	terminal := NewTerminal(&output)
	event := testEvent(StateFailed)
	event.Error = &ErrorInfo{Message: "request https://example.test/video?token=secret failed"}

	// When
	if err := terminal.Write(event); err != nil {
		t.Fatal(err)
	}

	// Then
	want := "job_id=job-7 state=failed name=clip.mp4 bytes=12/20 output=/tmp/clip.mp4 error=request https://example.test/video failed\n"
	if output.String() != want {
		t.Fatalf("got %q, want %q", output.String(), want)
	}
}

func TestTerminal_Write_keeps_untrusted_content_on_one_line(t *testing.T) {
	// Given
	var output strings.Builder
	terminal := NewTerminal(&output)
	event := testEvent(StateSucceeded)
	event.Name = "clip\nforged=success"

	// When
	if err := terminal.Write(event); err != nil {
		t.Fatal(err)
	}

	// Then
	if strings.Count(output.String(), "\n") != 1 || strings.Contains(output.String(), "forged=success\n") {
		t.Fatalf("terminal output was injectable: %q", output.String())
	}
}

func TestLogger_Emit_webhook_500_is_non_fatal_and_logged_separately(t *testing.T) {
	// Given
	var payload Event
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode webhook payload: %v", err)
		}
		http.Error(writer, "no", http.StatusInternalServerError)
	}))
	defer server.Close()
	directory := t.TempDir()
	logger, err := New(Options{LogDir: directory, WebhookURL: server.URL, WebhookTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	// When
	if err := logger.Emit(context.Background(), testEvent(StateSucceeded)); err != nil {
		t.Fatalf("webhook failure became event failure: %v", err)
	}

	// Then
	data, err := os.ReadFile(filepath.Join(directory, "webhook-errors.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "status code 500") {
		t.Fatalf("webhook error was not recorded: %s", data)
	}
	if payload.URL != "https://example.test/video" {
		t.Fatalf("webhook URL leaked secret: %q", payload.URL)
	}
}

func TestLogger_Emit_webhook_timeout_is_bounded_and_non_fatal(t *testing.T) {
	// Given
	serverDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-serverDone
	}))
	defer server.Close()
	directory := t.TempDir()
	logger, err := New(Options{LogDir: directory, WebhookURL: server.URL, WebhookTimeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	// When
	started := time.Now()
	err = logger.Emit(context.Background(), testEvent(StateFailed))
	close(serverDone)

	// Then
	if err != nil {
		t.Fatalf("webhook timeout became event failure: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 150*time.Millisecond {
		t.Fatalf("webhook was not bounded: %v", elapsed)
	}
}

func TestLogger_Emit_writes_success_and_error_records_as_parseable_jsonl(t *testing.T) {
	// Given
	directory := t.TempDir()
	logger, err := New(Options{LogDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	// When
	if err := logger.Emit(context.Background(), testEvent(StateSucceeded)); err != nil {
		t.Fatal(err)
	}
	if err := logger.Emit(context.Background(), testEvent(StateFailed)); err != nil {
		t.Fatal(err)
	}

	// Then
	data, err := os.ReadFile(filepath.Join(directory, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d JSONL records, want 2", len(lines))
	}
	for index, line := range lines {
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("record %d is not JSON: %v", index, err)
		}
	}
}

func TestLogger_Emit_canceled_context_still_records_event(t *testing.T) {
	// Given
	directory := t.TempDir()
	logger, err := New(Options{LogDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// When
	err = logger.Emit(ctx, testEvent(StateCanceled))

	// Then
	if err != nil {
		t.Fatalf("canceled context blocked local logging: %v", err)
	}
}

var _ io.Writer = (*strings.Builder)(nil)
