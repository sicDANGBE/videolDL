package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRun_help_shows_global_worker_commands_when_requested(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "top level", args: []string{"--help"}, want: []string{"Commandes :"}},
		{name: "config", args: []string{"config", "--help"}, want: []string{"Usage: videodl config init|path|show|get|set|edit", "config set [flags] KEY VALUE"}},
		{name: "daemon", args: []string{"daemon", "--help"}, want: []string{"Usage: videodl daemon start|stop|status|restart|logs"}},
		{name: "completion", args: []string{"completion", "--help"}, want: []string{"Usage: videodl completion bash|zsh|fish"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			// When
			err := Run(context.Background(), Options{Args: tt.args, Out: &stdout, ErrOut: &stderr, Env: []string{}})

			// Then
			if err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			output := stdout.String() + stderr.String()
			for _, want := range tt.want {
				if !strings.Contains(output, want) {
					t.Fatalf("output = %q, want %q", output, want)
				}
			}
		})
	}
}
