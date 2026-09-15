package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfig_Load_uses_configPathOverride_before_file_load(t *testing.T) {
	// Given
	home := t.TempDir()
	work := t.TempDir()
	firstPath := filepath.Join(t.TempDir(), "first.json")
	selectedPath := filepath.Join(t.TempDir(), "selected.json")
	if err := os.WriteFile(firstPath, []byte(`{"concurrency":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(selectedPath, []byte(`{"concurrency":8}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// When
	got, err := Load(LoadOptions{
		HomeDir:    home,
		WorkingDir: work,
		ConfigPath: firstPath,
		Env:        []string{},
		Overrides:  Overrides{ConfigPath: selectedPath},
	})

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if got.Concurrency != 8 {
		t.Fatalf("concurrency = %d, want 8 from override-selected file", got.Concurrency)
	}
	if got.ConfigPath != selectedPath {
		t.Fatalf("config path = %q, want %q", got.ConfigPath, selectedPath)
	}
}

func TestConfig_Load_rejects_invalidJSON(t *testing.T) {
	// Given
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"concurrency":`), 0o600); err != nil {
		t.Fatal(err)
	}

	// When
	_, err := Load(LoadOptions{HomeDir: t.TempDir(), WorkingDir: t.TempDir(), ConfigPath: configPath, Env: []string{}})

	// Then
	if !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("error = %v, want ErrInvalidJSON", err)
	}
}

func TestConfig_Load_does_not_expose_malformedJSONValue(t *testing.T) {
	// Given
	configPath := filepath.Join(t.TempDir(), "config.json")
	secret := "super-secret-token"
	if err := os.WriteFile(configPath, []byte(`{"destination":"`+secret), 0o600); err != nil {
		t.Fatal(err)
	}

	// When
	_, err := Load(LoadOptions{HomeDir: t.TempDir(), WorkingDir: t.TempDir(), ConfigPath: configPath, Env: []string{}})

	// Then
	if !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("error = %v, want ErrInvalidJSON", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("error exposed malformed config value")
	}
}

func TestConfig_Load_rejects_unknownJSONField(t *testing.T) {
	// Given
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"secret":"do-not-log"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// When
	_, err := Load(LoadOptions{HomeDir: t.TempDir(), WorkingDir: t.TempDir(), ConfigPath: configPath, Env: []string{}})

	// Then
	if !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("error = %v, want ErrInvalidJSON", err)
	}
	if strings.Contains(err.Error(), "do-not-log") {
		t.Fatal("error exposed a config value")
	}
}

func TestConfig_Load_rejects_invalidEnvironmentConcurrency(t *testing.T) {
	// Given
	secret := "super-secret-token"
	env := []string{"VIDEODL_CONCURRENCY=" + secret}

	// When
	_, err := Load(LoadOptions{HomeDir: t.TempDir(), WorkingDir: t.TempDir(), Env: env})

	// Then
	if !errors.Is(err, ErrInvalidConcurrency) {
		t.Fatalf("error = %v, want ErrInvalidConcurrency", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("error exposed malformed environment value")
	}
}

func TestConfig_Load_is_deterministic_for_repeated_reads(t *testing.T) {
	// Given
	home := t.TempDir()
	work := t.TempDir()
	opts := LoadOptions{HomeDir: home, WorkingDir: work, Env: []string{"VIDEODL_DESTINATION=downloads"}}

	// When
	first, firstErr := Load(opts)
	second, secondErr := Load(opts)

	// Then
	if firstErr != nil || secondErr != nil {
		t.Fatalf("repeated loads returned errors: %v, %v", firstErr, secondErr)
	}
	if first != second {
		t.Fatalf("repeated loads differ: %#v != %#v", first, second)
	}
}

func TestConfig_Load_rejects_invalidConcurrency(t *testing.T) {
	// Given
	for _, value := range []int{0, 9} {
		// When
		_, err := Load(LoadOptions{HomeDir: t.TempDir(), WorkingDir: t.TempDir(), Env: []string{}, Overrides: Overrides{Concurrency: intPtr(value)}})

		// Then
		if !errors.Is(err, ErrInvalidConcurrency) {
			t.Errorf("concurrency %d: error = %v, want ErrInvalidConcurrency", value, err)
		}
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Field != "concurrency" {
			t.Errorf("concurrency %d: error = %v, want typed concurrency validation", value, err)
		}
	}
}

func TestConfig_Load_accepts_concurrency_boundaries(t *testing.T) {
	// Given
	for _, value := range []int{1, 8} {
		// When
		got, err := Load(LoadOptions{HomeDir: t.TempDir(), WorkingDir: t.TempDir(), Env: []string{}, Overrides: Overrides{Concurrency: intPtr(value)}})

		// Then
		if err != nil {
			t.Errorf("concurrency %d: unexpected error: %v", value, err)
			continue
		}
		if got.Concurrency != value {
			t.Errorf("concurrency = %d, want %d", got.Concurrency, value)
		}
	}
}
