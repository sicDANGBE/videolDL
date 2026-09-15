package observability

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type notifyCommand struct {
	executable string
	args       []string
	env        []string
}

func newNotifyCommand(raw string, env []string) (*notifyCommand, error) {
	fields, err := parseNotifyCommand(raw)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, nil
	}
	return &notifyCommand{executable: fields[0], args: append([]string(nil), fields[1:]...), env: envForCommand(env)}, nil
}

func (command *notifyCommand) Deliver(ctx context.Context, event Event) error {
	args := make([]string, 0, len(command.args))
	for _, arg := range command.args {
		args = append(args, notifyArgument(arg, event))
	}
	process := exec.CommandContext(ctx, command.executable, args...)
	process.Env = command.env
	process.Stdout = io.Discard
	process.Stderr = io.Discard
	if err := process.Run(); err != nil {
		return fmt.Errorf("run notify_command: %w", err)
	}
	return nil
}

func envForCommand(env []string) []string {
	if env == nil {
		return nil
	}
	return append([]string(nil), env...)
}

func notifyArgument(value string, event Event) string {
	errorMessage := ""
	if event.Error != nil {
		errorMessage = event.Error.Message
	}
	replacer := strings.NewReplacer(
		"{job_id}", event.JobID,
		"{state}", string(event.State),
		"{name}", event.Name,
		"{output_path}", event.OutputPath,
		"{url}", event.URL,
		"{error}", errorMessage,
	)
	return replacer.Replace(value)
}

func parseNotifyCommand(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var fields []string
	var builder strings.Builder
	var quote rune
	for _, char := range trimmed {
		switch {
		case quote != 0 && char == quote:
			quote = 0
		case quote != 0:
			builder.WriteRune(char)
		case char == '\'' || char == '"':
			quote = char
		case char == ' ' || char == '\t' || char == '\n' || char == '\r':
			if builder.Len() > 0 {
				fields = append(fields, builder.String())
				builder.Reset()
			}
		default:
			builder.WriteRune(char)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("notify_command has an unterminated quote")
	}
	if builder.Len() > 0 {
		fields = append(fields, builder.String())
	}
	return fields, nil
}
