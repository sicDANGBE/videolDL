package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"video-downloader/internal/config"
	"video-downloader/internal/queue"
)

var completionCommands = []string{"setup", "doctor", "help", "man", "add", "worker", "list", "status", "retry", "cancel", "config", "daemon", "watch", "completion"}
var completionShells = []string{"bash", "zsh", "fish"}
var completionFlags = []string{"--retries", "--resume", "--max-height", "--idle-timeout", "--name", "--output", "--watch", "--config", "--state", "--log", "--destination", "--concurrency", "--timeout", "--ffmpeg", "--ffmpeg-path", "--webhook", "--json", "--once", "--interval", "--help"}
var completionConfigKeys = []string{"min_free_space", "retries", "resume", "max_height", "idle_timeout", "state_path", "log_path", "destination", "concurrency", "timeout", "ffmpeg", "ffmpeg_path", "webhook_url", "editor", "auto_start_worker", "daemon_pid_path", "daemon_log_path", "notify_command"}

func runCompletion(options Options, args []string) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, err := fmt.Fprintln(options.Out, "Usage: videodl completion bash|zsh|fish")
		return err
	}
	if len(args) != 1 {
		return fmt.Errorf("usage: videodl completion bash|zsh|fish")
	}
	script, err := completionScript(args[0])
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(options.Out, script)
	return err
}

func runHiddenCompletion(ctx context.Context, options Options, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: videodl __complete SHELL [WORDS...]")
	}
	if !supportedShell(args[0]) {
		return fmt.Errorf("unsupported shell %q", args[0])
	}
	for _, suggestion := range completionSuggestions(ctx, options, args[1:]) {
		if _, err := fmt.Fprintln(options.Out, suggestion); err != nil {
			return err
		}
	}
	return nil
}

func completionScript(shell string) (string, error) {
	commands := strings.Join(completionCommands, " ")
	flags := strings.Join(completionFlags, " ")
	switch shell {
	case "bash":
		return fmt.Sprintf(`_videodl_completion() {
  local suggestions
  local executable="${COMP_WORDS[0]}"
  suggestions=$("$executable" __complete bash "${COMP_WORDS[@]:1:COMP_CWORD}" 2>/dev/null)
  COMPREPLY=( $(compgen -W "$suggestions" -- "${COMP_WORDS[COMP_CWORD]}") )
}
complete -F _videodl_completion videodl
# commands: %s
# flags: %s
`, commands, flags), nil
	case "zsh":
		return fmt.Sprintf(`#compdef videodl
_videodl_completion() {
  local -a suggestions
  suggestions=(${(f)"$("${words[1]}" __complete zsh "${(@)words[2,CURRENT]}" 2>/dev/null)"})
  compadd -- $suggestions
}
compdef _videodl_completion videodl
# commands: %s
# flags: %s
`, commands, flags), nil
	case "fish":
		return fmt.Sprintf(`function __videodl_completion
  command videodl __complete fish (commandline -opc)[2..-1] (commandline -ct)
end
complete -c videodl -f -a '(__videodl_completion)'
# commands: %s
# flags: %s
`, commands, flags), nil
	default:
		return "", fmt.Errorf("unsupported shell %q", shell)
	}
}

func completionSuggestions(ctx context.Context, options Options, words []string) []string {
	current := completionCurrent(words)
	command := completionCommand(words)
	if current == "" && command == "" {
		return matchingCompletions(completionCommands, current)
	}
	if command == "" {
		return matchingCompletions(completionCommands, current)
	}
	if strings.HasPrefix(current, "-") {
		return matchingCompletions(completionFlags, current)
	}
	switch command {
	case "help":
		return matchingCompletions(completionCommands, current)
	case "completion":
		return matchingCompletions(completionShells, current)
	case "config":
		return configCompletion(words, current)
	case "daemon":
		return matchingCompletions([]string{"start", "stop", "status", "restart", "logs"}, current)
	case "status", "retry", "cancel":
		ids := completionJobIDs(ctx, options, words)
		return matchingCompletions(ids, current)
	case "setup":
		return matchingCompletions([]string{"--config", "--destination", "--help"}, current)
	case "doctor":
		return matchingCompletions([]string{"--config", "--help"}, current)
	case "add", "worker", "list", "watch":
		return matchingCompletions(completionFlags, current)
	default:
		return nil
	}
}

func completionCurrent(words []string) string {
	if len(words) == 0 {
		return ""
	}
	return words[len(words)-1]
}

func completionCommand(words []string) string {
	for _, word := range words {
		if strings.HasPrefix(word, "-") || strings.TrimSpace(word) == "" {
			continue
		}
		if isCompletionCommand(word) {
			return word
		}
		return ""
	}
	return ""
}

func configCompletion(words []string, current string) []string {
	subcommands := []string{"init", "path", "show", "get", "set", "edit"}
	if len(words) < 3 {
		return matchingCompletions(subcommands, current)
	}
	subcommand := words[1]
	switch subcommand {
	case "get", "set":
		return matchingCompletions(completionConfigKeys, current)
	case "init", "path", "show", "edit":
		return matchingCompletions(completionFlags, current)
	default:
		return matchingCompletions(subcommands, current)
	}
}

func completionJobIDs(ctx context.Context, options Options, words []string) []string {
	loaded, err := completionConfig(options, words)
	if err != nil {
		return nil
	}
	jobs, err := queue.Snapshot(loaded.StatePath)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(jobs))
	for _, job := range jobs {
		ids = append(ids, job.ID)
	}
	return ids
}

func completionConfig(options Options, words []string) (config.Config, error) {
	workingDir := options.WorkingDir
	if workingDir == "" {
		workingDir = "."
	}
	if !filepath.IsAbs(workingDir) {
		absolute, err := filepath.Abs(workingDir)
		if err != nil {
			return config.Config{}, fmt.Errorf("resolve working directory: %w", err)
		}
		workingDir = absolute
	}
	overrides := config.Overrides{}
	configPath := ""
	for index := 0; index < len(words); index++ {
		word := words[index]
		if index+1 >= len(words) {
			continue
		}
		value := words[index+1]
		switch word {
		case "--config":
			configPath = value
			overrides.ConfigPath = value
		case "--state":
			overrides.StatePath = &value
		case "--log":
			overrides.LogPath = &value
		case "--destination":
			overrides.Destination = &value
		}
	}
	return config.Load(config.LoadOptions{ConfigPath: configPath, HomeDir: options.HomeDir, WorkingDir: workingDir, Env: options.Env, Overrides: overrides})
}

func matchingCompletions(values []string, prefix string) []string {
	matches := make([]string, 0, len(values))
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			matches = append(matches, value)
		}
	}
	return matches
}

func supportedShell(shell string) bool {
	for _, candidate := range completionShells {
		if shell == candidate {
			return true
		}
	}
	return false
}

func isCompletionCommand(command string) bool {
	for _, candidate := range completionCommands {
		if command == candidate {
			return true
		}
	}
	return false
}
