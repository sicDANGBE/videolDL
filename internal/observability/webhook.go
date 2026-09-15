package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Webhook struct {
	url     string
	client  *http.Client
	timeout time.Duration
}

func newWebhook(rawURL string, timeout time.Duration) (*Webhook, error) {
	if rawURL == "" {
		return nil, nil
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Webhook{url: rawURL, client: &http.Client{}, timeout: timeout}, nil
}

func (webhook *Webhook) deliver(ctx context.Context, event Event) error {
	payload, err := json.Marshal(event.safe())
	if err != nil {
		return fmt.Errorf("marshal webhook event: %w", err)
	}
	requestContext, cancel := context.WithTimeout(ctx, webhook.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, webhook.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := webhook.client.Do(request)
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("webhook returned status code %d", response.StatusCode)
	}
	return nil
}
