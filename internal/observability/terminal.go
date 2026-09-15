package observability

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

type Terminal struct {
	mu  sync.Mutex
	out io.Writer
}

func NewTerminal(out io.Writer) *Terminal {
	return &Terminal{out: out}
}

func (terminal *Terminal) Write(event Event) error {
	if err := event.valid(); err != nil {
		return err
	}
	safe := event.safe()
	errorText := ""
	if safe.Error != nil {
		errorText = terminalText(safe.Error.Message)
	}
	terminal.mu.Lock()
	defer terminal.mu.Unlock()
	_, err := fmt.Fprintf(terminal.out, "job_id=%s state=%s name=%s bytes=%d/%d output=%s error=%s\n", terminalText(safe.JobID), safe.State, terminalText(safe.Name), safe.BytesDownloaded, safe.BytesTotal, terminalText(safe.OutputPath), errorText)
	return err
}

func terminalText(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(value)
}
