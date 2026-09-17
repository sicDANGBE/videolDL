package cli

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"video-downloader/internal/config"
	"video-downloader/internal/queue"
)

func optionValueCompletions(options Options, name, current string) []string {
	var suggestions []string
	switch name {
	case "config", "state", "ffmpeg-path":
		return pathCompletions(options, current, false)
	case "destination", "log":
		return pathCompletions(options, current, true)
	case "resume", "ffmpeg", "json", "watch", "once":
		suggestions = []string{"true", "false"}
	case "concurrency":
		for i := 1; i <= 8; i++ {
			suggestions = append(suggestions, strconv.Itoa(i))
		}
	case "retries":
		for i := 0; i <= 10; i++ {
			suggestions = append(suggestions, strconv.Itoa(i))
		}
	case "max-height":
		suggestions = []string{"0", "360", "480", "720", "1080", "1440", "2160", "4320"}
	case "timeout", "idle-timeout", "interval":
		suggestions = []string{"1s", "2s", "5s", "15s", "30s", "60s"}
	}
	return matchingCompletions(suggestions, current)
}
func configValueCompletions(options Options, key, current string) []string {
	switch key {
	case "state_path", "daemon_pid_path", "daemon_log_path", "ffmpeg_path":
		return pathCompletions(options, current, false)
	case "destination", "log_path":
		return pathCompletions(options, current, true)
	case "auto_start_worker", "resume", "ffmpeg":
		return matchingCompletions([]string{"true", "false"}, current)
	case "min_free_space":
		return matchingCompletions([]string{"0", "512MiB", "1GiB", "2GiB", "4GiB"}, current)
	default:
		return optionValueCompletions(options, strings.ReplaceAll(key, "_", "-"), current)
	}
}
func pathCompletions(options Options, current string, directoriesOnly bool) []string {
	if strings.HasPrefix(current, "-") {
		return nil
	}
	prefix, base := filepath.Split(current)
	directory := prefix
	if strings.HasPrefix(directory, "~/") {
		home := options.HomeDir
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		directory = filepath.Join(home, strings.TrimPrefix(directory, "~/"))
	}
	if !filepath.IsAbs(directory) {
		wd, err := configWorkingDir(options)
		if err != nil {
			return nil
		}
		directory = filepath.Join(wd, directory)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	var suggestions []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), base) || strings.ContainsAny(entry.Name(), "\n\r\t") {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") && !strings.HasPrefix(base, ".") {
			continue
		}
		isDir := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			info, err := os.Stat(filepath.Join(directory, entry.Name()))
			isDir = err == nil && info.IsDir()
		}
		if directoriesOnly && !isDir {
			continue
		}
		value := prefix + entry.Name()
		if isDir {
			value += "/"
		}
		suggestions = append(suggestions, value)
	}
	return suggestions
}
func completionJobIDs(ctx context.Context, options Options, values map[string]string) []string {
	if ctx.Err() != nil {
		return nil
	}
	wd, err := configWorkingDir(options)
	if err != nil {
		return nil
	}
	path := values["config"]
	if path != "" && !filepath.IsAbs(path) {
		path = filepath.Join(wd, path)
	}
	overrides := config.Overrides{}
	if state, ok := values["state"]; ok {
		overrides.StatePath = &state
	}
	loaded, err := config.Load(config.LoadOptions{ConfigPath: path, HomeDir: options.HomeDir, WorkingDir: wd, Env: options.Env, Overrides: overrides})
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
