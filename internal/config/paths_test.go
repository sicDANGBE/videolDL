package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConfig_ResolveDestination_returns_absolute_path(t *testing.T) {
	// Given
	workingDir := filepath.Join(t.TempDir(), "work")
	if err := os.Mkdir(workingDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// When
	got, err := ResolveDestination("downloads/video.mp4", workingDir)

	// Then
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(workingDir, "downloads", "video.mp4")
	if got != want {
		t.Fatalf("destination = %q, want %q", got, want)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("destination %q is not absolute", got)
	}
}

func TestConfig_ResolveDestination_rejects_escape(t *testing.T) {
	// Given
	workingDir := t.TempDir()

	// When
	_, err := ResolveDestination("../downloads", workingDir)

	// Then
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("error = %v, want ErrInvalidPath", err)
	}
}

func TestConfig_ResolveDestination_accepts_absoluteOutsideWorkingDir(t *testing.T) {
	// Given
	workingDir := t.TempDir()
	outside := filepath.Join(filepath.Dir(workingDir), "downloads")

	// When
	got, err := ResolveDestination(outside, workingDir)

	// Then
	if err != nil {
		t.Fatalf("error = %v, want absolute path outside working directory accepted", err)
	}
	if got != outside {
		t.Fatalf("destination = %q, want %q", got, outside)
	}
}

func TestConfig_ResolveDestination_accepts_absoluteInsideWorkingDir(t *testing.T) {
	// Given
	workingDir := t.TempDir()
	destination := filepath.Join(workingDir, "downloads")

	// When
	got, err := ResolveDestination(destination, workingDir)

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if got != destination {
		t.Fatalf("destination = %q, want %q", got, destination)
	}
}
