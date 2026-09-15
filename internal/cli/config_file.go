package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"video-downloader/internal/config"
)

func loadForConfigCommand(options Options, flags configCommandFlags, workingDir string, explicit bool) (string, config.Config, error) {
	overrides := config.Overrides{}
	if flags.configPath != "" {
		overrides.ConfigPath = flags.configPath
	}
	loaded, err := config.Load(config.LoadOptions{ConfigPath: flags.configPath, HomeDir: options.HomeDir, WorkingDir: workingDir, Env: options.Env, Overrides: overrides})
	if err != nil && (explicit || !errors.Is(err, fs.ErrNotExist)) {
		return "", config.Config{}, err
	}
	if err == nil {
		return loaded.ConfigPath, loaded, nil
	}
	path := selectedConfigPath(flags, options.Env)
	defaults, defaultErr := config.Load(config.LoadOptions{HomeDir: options.HomeDir, WorkingDir: workingDir, Env: envWithoutConfigPath(options.Env)})
	if defaultErr != nil {
		return "", config.Config{}, defaultErr
	}
	defaults.ConfigPath = path
	return path, defaults, nil
}

func envWithoutConfigPath(env []string) []string {
	items := env
	if env == nil {
		items = os.Environ()
	}
	filtered := make([]string, 0, len(items))
	for _, item := range items {
		if !strings.HasPrefix(item, "VIDEODL_CONFIG=") {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func configWorkingDir(options Options) (string, error) {
	workingDir := options.WorkingDir
	if workingDir == "" {
		var err error
		workingDir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve working directory: %w", err)
		}
	}
	if !filepath.IsAbs(workingDir) {
		return "", fmt.Errorf("working directory must be absolute")
	}
	return workingDir, nil
}

func writePersistedConfig(path string, stored persistedConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')
	if err := writeConfigAtomic(path, data); err != nil {
		return fmt.Errorf("write config file %q: %w", path, err)
	}
	return nil
}

func ensureConfigFile(path string, stored persistedConfig) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat config file %q: %w", path, err)
	}
	return writePersistedConfig(path, stored)
}

func updateConfigFile(path string, stored persistedConfig, options Options, flags configCommandFlags, workingDir string) error {
	var before []byte
	data, err := os.ReadFile(path)
	if err == nil {
		before = data
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read config file %q: %w", path, err)
	}
	if err := writePersistedConfig(path, stored); err != nil {
		return err
	}
	if _, _, err := loadForConfigCommand(options, flags, workingDir, true); err != nil {
		if before != nil {
			return errors.Join(fmt.Errorf("validate config: %w", err), writeConfigAtomic(path, before))
		}
		return errors.Join(fmt.Errorf("validate config: %w", err), os.Remove(path))
	}
	return nil
}

func writeConfigAtomic(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}
