package observability

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogger_Emit_redacts_url_credentials_from_all_outputs(t *testing.T) {
	// Given
	directory := t.TempDir()
	var terminalOutput strings.Builder
	logger, err := New(Options{LogDir: directory, Terminal: &terminalOutput})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	event := testEvent(StateFailed)
	event.URL = "https://user:secret@example.test/video?token=query#fragment"
	event.Error = &ErrorInfo{Message: "first https://alice:alicepass@example.test/a?token=one#frag second https://carol:carolpass@example.test/c?token=three#frag third http://bob:bobpass@example.test/b?key=two#part fourth http://dave:davepass@example.test/d?key=four#part"}

	// When
	if err := logger.Emit(context.Background(), event); err != nil {
		t.Fatal(err)
	}

	// Then
	data, err := os.ReadFile(filepath.Join(directory, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	combined := string(data) + terminalOutput.String()
	for _, secret := range []string{"user", "secret", "query", "fragment", "alice", "one", "frag", "carol", "three", "bob", "two", "key", "part", "dave", "alicepass", "carolpass", "bobpass", "davepass"} {
		if strings.Contains(combined, secret) {
			t.Fatalf("URL credential or component leaked (%q): %s", secret, combined)
		}
	}
	if strings.Contains(combined, "https://alice") || strings.Contains(combined, "https://carol") || strings.Contains(combined, "http://bob") || strings.Contains(combined, "http://dave") {
		t.Fatalf("URL credentials or components leaked: %s", combined)
	}
	if !strings.Contains(combined, "https://example.test/video") {
		t.Fatalf("redacted URL was not preserved: %s", combined)
	}
}

func TestEvent_valid_accepts_only_consistent_success_and_failure_states(t *testing.T) {
	// Given
	cases := []struct {
		name  string
		state State
		err   *ErrorInfo
		valid bool
	}{
		{name: "success without error", state: StateSucceeded, valid: true},
		{name: "failure with error", state: StateFailed, err: &ErrorInfo{Message: "failed"}, valid: true},
		{name: "success with error", state: StateSucceeded, err: &ErrorInfo{Message: "contradiction"}},
		{name: "failure without error", state: StateFailed},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			event := testEvent(testCase.state)
			event.Error = testCase.err

			// When
			err := event.valid()

			// Then
			if (err == nil) != testCase.valid {
				t.Fatalf("valid() error = %v, valid = %t", err, testCase.valid)
			}
		})
	}
}

func TestWebhook_payload_redacts_url_credentials(t *testing.T) {
	// Given
	var payload Event
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode webhook payload: %v", err)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	logger, err := New(Options{LogDir: t.TempDir(), WebhookURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	event := testEvent(StateSucceeded)
	event.URL = "https://user:secret@example.test/video?token=query#fragment"

	// When
	if err := logger.Emit(context.Background(), event); err != nil {
		t.Fatal(err)
	}

	// Then
	if payload.URL != "https://example.test/video" {
		t.Fatalf("webhook URL was not redacted: %q", payload.URL)
	}
}

func TestLogger_Emit_preserves_stale_jsonl_records_when_appending(t *testing.T) {
	// Given
	directory := t.TempDir()
	stale := `{"job_id":"old","url":"https://example.test/old","name":"old.mp4","output_path":"/tmp/old.mp4","state":"succeeded","started_at":"2026-09-09T12:00:00Z","finished_at":"2026-09-09T12:00:01Z","bytes_downloaded":1,"bytes_total":1}` + "\n"
	if err := os.WriteFile(filepath.Join(directory, "events.jsonl"), []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	logger, err := New(Options{LogDir: directory})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	// When
	if err := logger.Emit(context.Background(), testEvent(StateSucceeded)); err != nil {
		t.Fatal(err)
	}

	// Then
	data, err := os.ReadFile(filepath.Join(directory, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if records := strings.Count(string(data), "\n"); records != 2 {
		t.Fatalf("got %d records after append, want 2", records)
	}
}

func TestLogger_Emit_repeated_webhook_interruptions_remain_non_fatal(t *testing.T) {
	// Given
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	directory := t.TempDir()
	logger, err := New(Options{LogDir: directory, WebhookURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()

	// When
	for i := 0; i < 3; i++ {
		if err := logger.Emit(context.Background(), testEvent(StateSucceeded)); err != nil {
			t.Fatalf("interrupted webhook changed job status: %v", err)
		}
	}

	// Then
	data, err := os.ReadFile(filepath.Join(directory, "webhook-errors.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if records := strings.Count(string(data), "\n"); records != 3 {
		t.Fatalf("got %d webhook error records, want 3", records)
	}
}

func TestLogger_Close_closes_events_when_webhook_close_fails(t *testing.T) {
	// Given
	logger, err := New(Options{LogDir: t.TempDir(), WebhookURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Emit(context.Background(), testEvent(StateSucceeded)); err != nil {
		t.Fatal(err)
	}
	if _, err := logger.events.WriteString("event\n"); err != nil {
		t.Fatal(err)
	}
	if logger.webhooks != nil {
		if err := logger.webhooks.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Fatal("expected webhook file to be opened")
	}

	// When
	closeErr := logger.Close()

	// Then
	if closeErr == nil {
		t.Fatal("Close returned nil after webhook close failure")
	}
	if _, err := logger.events.WriteString("after-close\n"); err == nil {
		t.Fatal("events file remained open after webhook close failure")
	}
}
