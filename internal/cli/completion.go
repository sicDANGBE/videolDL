package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
)

var completionCommands = []string{"setup", "doctor", "help", "man", "add", "worker", "list", "status", "retry", "cancel", "config", "daemon", "watch", "completion"}
var completionShells = []string{"bash", "zsh", "fish"}

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
	flags := strings.Join(allCompletionFlags(), " ")
	switch shell {
	case "bash":
		return fmt.Sprintf(`_videodl_completion() {
  local suggestion
  local executable="${COMP_WORDS[0]}"
  COMPREPLY=()
  while IFS= read -r suggestion; do
    [[ -z "$suggestion" ]] && continue
    COMPREPLY+=("$suggestion")
    # Path suggestions may contain spaces or shell metacharacters.
    if [[ "$suggestion" =~ [^a-zA-Z0-9_.=-] ]]; then
      compopt -o filenames 2>/dev/null || true
    fi
    if [[ "$suggestion" == */ ]]; then
      compopt -o nospace 2>/dev/null || true
    fi
  done < <("$executable" __complete bash "${COMP_WORDS[@]:1:COMP_CWORD}" 2>/dev/null)
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

// The final word is the token being completed, including an empty token after a space.
func completionSuggestions(ctx context.Context, options Options, words []string) []string {
	if len(words) == 0 {
		return completionCommands
	}
	current := words[len(words)-1]
	if len(words) == 1 && !strings.HasPrefix(current, "-") {
		return matchingCompletions(completionCommands, current)
	}
	command := ""
	args := words
	if isCompletionCommand(words[0]) {
		command = words[0]
		args = words[1:]
	}
	if command == "config" || command == "daemon" {
		subs := []string{"init", "path", "show", "get", "set", "edit"}
		if command == "daemon" {
			subs = []string{"start", "stop", "status", "restart", "logs"}
		}
		if len(args) <= 1 {
			if strings.HasPrefix(current, "-") {
				return matchingCompletions([]string{"--help", "-h"}, current)
			}
			return matchingCompletions(subs, current)
		}
		known := false
		for _, sub := range subs {
			if args[0] == sub {
				known = true
			}
		}
		if !known {
			return nil
		}
		command += " " + args[0]
		args = args[1:]
	}
	if len(args) == 0 {
		args = []string{""}
		current = ""
	}
	if current == "=" && len(args) > 1 && strings.HasPrefix(args[len(args)-2], "-") {
		args = append(append([]string(nil), args...), "")
		current = ""
	}
	set := commandFlagSet(command, &commandFlags{}, io.Discard)
	state := scanCompletion(set, args[:len(args)-1])
	if !state.valid || state.values["help"] == "true" {
		return nil
	}
	// Bash normally splits '=' into a word of its own. Keep values separate
	// when replacing that word, while also accepting intact --option=value tokens.
	if state.pending != "" {
		if current == "=" {
			current = ""
		}
		return optionValueCompletions(options, state.pending, current)
	}
	if !state.ended && strings.HasPrefix(current, "-") {
		if key, value, ok := strings.Cut(current, "="); ok {
			option := set.Lookup(strings.TrimLeft(key, "-"))
			if option == nil {
				return nil
			}
			suggestions := optionValueCompletions(options, canonicalOption(option.Name), value)
			for i := range suggestions {
				suggestions[i] = key + "=" + suggestions[i]
			}
			return suggestions
		}
		return matchingCompletions(availableCompletionFlags(set, state), current)
	}
	var suggestions []string
	switch command {
	case "help":
		if len(state.positionals) == 0 {
			for _, name := range completionCommands {
				if name != "help" {
					suggestions = append(suggestions, name)
				}
			}
		}
	case "completion":
		if len(state.positionals) == 0 {
			suggestions = completionShells
		}
	case "config get", "config set":
		if len(state.positionals) == 0 {
			suggestions = completionConfigKeys
		} else if command == "config set" && len(state.positionals) == 1 {
			return configValueCompletions(options, state.positionals[0], current)
		}
	case "status", "retry", "cancel":
		if len(state.positionals) == 0 {
			suggestions = completionJobIDs(ctx, options, state.values)
		}
	}
	// Options cannot follow the first positional argument with Go's flag parser.
	// Free-text output names and URLs deliberately have no invented suggestions.
	if current == "" && !state.ended {
		suggestions = append(suggestions, availableCompletionFlags(set, state)...)
	}
	return matchingCompletions(suggestions, current)
}

type completionState struct {
	valid, ended bool
	pending      string
	values       map[string]string
	positionals  []string
}

func scanCompletion(set *flag.FlagSet, words []string) completionState {
	state := completionState{valid: true, values: map[string]string{}}
	for i := 0; i < len(words); i++ {
		word := words[i]
		if state.pending != "" {
			if word == "=" {
				continue
			}
			state.values[state.pending] = word
			state.pending = ""
			continue
		}
		if state.ended || word == "-" || !strings.HasPrefix(word, "-") {
			state.positionals = append(state.positionals, word)
			state.ended = true
			continue
		}
		if word == "--" {
			state.ended = true
			continue
		}
		key, value, inline := strings.Cut(strings.TrimLeft(word, "-"), "=")
		option := set.Lookup(key)
		if option == nil {
			state.valid = false
			return state
		}
		key = canonicalOption(option.Name)
		if inline {
			state.values[key] = value
			continue
		}
		if i+1 < len(words) && words[i+1] == "=" {
			state.pending = key
			i++
			continue
		}
		if booleanOption(option) {
			state.values[key] = "true"
		} else {
			state.pending = key
		}
	}
	return state
}
func availableCompletionFlags(set *flag.FlagSet, state completionState) []string {
	var flags []string
	set.VisitAll(func(value *flag.Flag) {
		name := canonicalOption(value.Name)
		if _, used := state.values[name]; used {
			return
		}
		if name == "name" || name == "output" {
			if _, used := state.values["name"]; used {
				return
			}
			if _, used := state.values["output"]; used {
				return
			}
		}
		prefix := "--"
		if name != value.Name {
			prefix = "-"
		}
		flags = append(flags, prefix+value.Name)
	})
	return flags
}
func allCompletionFlags() []string {
	seen := map[string]bool{}
	commands := append([]string{"", "config show", "config edit", "daemon start"}, completionCommands...)
	for _, command := range commands {
		set := commandFlagSet(command, &commandFlags{}, io.Discard)
		for _, f := range availableCompletionFlags(set, completionState{values: map[string]string{}}) {
			seen[f] = true
		}
	}
	var flags []string
	for f := range seen {
		flags = append(flags, f)
	}
	sort.Strings(flags)
	return flags
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
