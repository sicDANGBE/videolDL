package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_configInit_creates_default_config_without_overwriting(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	configPath := filepath.Join(home, ".config", "videodl", "config.json")
	var output bytes.Buffer

	// When
	err := Run(context.Background(), Options{Args: []string{"config", "init"}, Out: &output, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}})

	// Then
	if err != nil {
		t.Fatalf("config init returned error: %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("default config is invalid JSON: %v", err)
	}
	if decoded["destination"] != filepath.Join(home, "Downloads", "videodl") {
		t.Fatalf("destination = %#v", decoded["destination"])
	}
	if err := Run(context.Background(), Options{Args: []string{"config", "init"}, Out: &output, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}}); err == nil {
		t.Fatal("second config init unexpectedly overwrote existing config")
	}
}

func TestRun_configPath_prints_selected_config_path(t *testing.T) {
	// Given
	workDir := t.TempDir()
	selected := filepath.Join(workDir, "profile.json")
	var output bytes.Buffer

	// When
	err := Run(context.Background(), Options{Args: []string{"config", "path", "--config", selected}, Out: &output, ErrOut: ioDiscard{}, WorkingDir: workDir})

	// Then
	if err != nil {
		t.Fatalf("config path returned error: %v", err)
	}
	if strings.TrimSpace(output.String()) != selected {
		t.Fatalf("path output = %q, want %q", output.String(), selected)
	}
}

func TestRun_configInit_creates_environment_selected_config(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	selected := filepath.Join(t.TempDir(), "profiles", "custom.json")
	var output bytes.Buffer

	// When
	err := Run(context.Background(), Options{Args: []string{"config", "init"}, Out: &output, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{"VIDEODL_CONFIG=" + selected}})

	// Then
	if err != nil {
		t.Fatalf("config init returned error: %v", err)
	}
	if _, err := os.ReadFile(selected); err != nil {
		t.Fatalf("read selected config: %v", err)
	}
}

func TestRun_configShow_outputs_human_and_json_formats(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	var human bytes.Buffer
	var asJSON bytes.Buffer

	// When
	humanErr := Run(context.Background(), Options{Args: []string{"config", "show"}, Out: &human, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}})
	jsonErr := Run(context.Background(), Options{Args: []string{"config", "show", "--json"}, Out: &asJSON, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}})

	// Then
	if humanErr != nil {
		t.Fatalf("config show returned error: %v", humanErr)
	}
	if jsonErr != nil {
		t.Fatalf("config show --json returned error: %v", jsonErr)
	}
	if !strings.Contains(human.String(), "destination") || !strings.Contains(human.String(), "auto_start_worker") {
		t.Fatalf("human output = %q", human.String())
	}
	var decoded struct {
		Destination     string `json:"destination"`
		AutoStartWorker bool   `json:"auto_start_worker"`
	}
	if err := json.Unmarshal(asJSON.Bytes(), &decoded); err != nil {
		t.Fatalf("decode json output: %v", err)
	}
	if decoded.Destination != filepath.Join(home, "Downloads", "videodl") {
		t.Fatalf("destination = %q", decoded.Destination)
	}
}

func TestRun_configSetAndGet_validates_strict_keys_and_normalizes_destination(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	configPath := filepath.Join(home, ".config", "videodl", "config.json")
	relativeDestination := filepath.Join("Videos", "..", "Videos", "clips")
	var output bytes.Buffer

	// When
	err := Run(context.Background(), Options{Args: []string{"config", "set", "destination", relativeDestination}, Out: &output, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}})

	// Then
	if err != nil {
		t.Fatalf("config set destination returned error: %v", err)
	}
	wantDestination := filepath.Join(workDir, "Videos", "clips")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), wantDestination) {
		t.Fatalf("config data = %s, want destination %q", data, wantDestination)
	}
	if _, err := os.Stat(wantDestination); err != nil {
		t.Fatalf("destination directory was not created: %v", err)
	}
	var got bytes.Buffer
	if err := Run(context.Background(), Options{Args: []string{"config", "get", "destination"}, Out: &got, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}}); err != nil {
		t.Fatalf("config get returned error: %v", err)
	}
	if strings.TrimSpace(got.String()) != wantDestination {
		t.Fatalf("get destination = %q, want %q", got.String(), wantDestination)
	}
	before := string(data)
	if err := Run(context.Background(), Options{Args: []string{"config", "set", "missing_key", "value"}, Out: &output, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}}); err == nil {
		t.Fatal("config set invalid key unexpectedly succeeded")
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config after invalid key: %v", err)
	}
	if string(after) != before {
		t.Fatalf("config changed after invalid key: %s", after)
	}
}

func TestRun_configSet_rejects_malformed_values_without_persisting(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	configPath := filepath.Join(home, ".config", "videodl", "config.json")
	var output bytes.Buffer
	if err := Run(context.Background(), Options{Args: []string{"config", "init"}, Out: &output, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}}); err != nil {
		t.Fatalf("config init returned error: %v", err)
	}
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	// When
	boolErr := Run(context.Background(), Options{Args: []string{"config", "set", "auto_start_worker", "not-bool"}, Out: &output, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}})
	timeoutErr := Run(context.Background(), Options{Args: []string{"config", "set", "timeout", "not-duration"}, Out: &output, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}})

	// Then
	if boolErr == nil {
		t.Fatal("invalid boolean unexpectedly succeeded")
	}
	if timeoutErr == nil {
		t.Fatal("invalid duration unexpectedly succeeded")
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config after invalid values: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("config changed after invalid values: %s", after)
	}
}

func TestRun_configEdit_creates_opens_validates_and_restores_on_malformed_json(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	editorLog := filepath.Join(t.TempDir(), "editor.log")
	validEditor := writeFakeEditor(t, "printf '%s\\n' \"$1\" >> \""+editorLog+"\"\npython3 - <<'PY' \"$1\"\nimport json, pathlib, sys\npath = pathlib.Path(sys.argv[1])\ndata = json.loads(path.read_text())\ndata['editor'] = 'configured-editor'\npath.write_text(json.dumps(data, indent=2) + '\\n')\nPY\n")
	invalidEditor := writeFakeEditor(t, "printf '{bad json' > \"$1\"\n")
	var output bytes.Buffer

	// When
	err := Run(context.Background(), Options{Args: []string{"config", "edit", "--editor", validEditor}, Out: &output, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}})

	// Then
	if err != nil {
		t.Fatalf("config edit returned error: %v", err)
	}
	logData, err := os.ReadFile(editorLog)
	if err != nil {
		t.Fatalf("read editor log: %v", err)
	}
	configPath := filepath.Join(home, ".config", "videodl", "config.json")
	if strings.TrimSpace(string(logData)) != configPath {
		t.Fatalf("editor argv log = %q, want %q", logData, configPath)
	}
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(before), "configured-editor") {
		t.Fatalf("config after edit = %s", before)
	}
	if err := Run(context.Background(), Options{Args: []string{"config", "edit", "--editor", invalidEditor}, Out: &output, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{}}); err == nil {
		t.Fatal("malformed JSON edit unexpectedly succeeded")
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config after bad edit: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("config was not restored after bad edit: %s", after)
	}
}

func writeFakeEditor(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "editor.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o755); err != nil {
		t.Fatalf("write fake editor: %v", err)
	}
	return path
}

func TestRun_configEdit_uses_visual_editor_before_environment_editor(t *testing.T) {
	// Given
	home := t.TempDir()
	workDir := t.TempDir()
	visualLog := filepath.Join(t.TempDir(), "visual.log")
	editorLog := filepath.Join(t.TempDir(), "editor.log")
	visual := writeFakeEditor(t, "printf visual > \""+visualLog+"\"\n")
	editor := writeFakeEditor(t, "printf editor > \""+editorLog+"\"\n")
	var output bytes.Buffer

	// When
	err := Run(context.Background(), Options{Args: []string{"config", "edit"}, Out: &output, ErrOut: ioDiscard{}, HomeDir: home, WorkingDir: workDir, Env: []string{"VISUAL=" + visual, "EDITOR=" + editor}})

	// Then
	if err != nil {
		t.Fatalf("config edit returned error: %v", err)
	}
	if _, err := os.ReadFile(visualLog); err != nil {
		t.Fatalf("visual editor did not run: %v", err)
	}
	if _, err := os.Stat(editorLog); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("EDITOR should not have run before VISUAL, stat err = %v", err)
	}
}
