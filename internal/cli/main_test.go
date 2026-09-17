package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests must never inherit a personal queue, auto-start preference or webhook.
func TestMain(m *testing.M) {
	if os.Getenv("VIDEODL_DAEMON_HELPER") == "1" {
		os.Exit(m.Run())
	}
	root, err := os.MkdirTemp("", "videodl-cli-tests-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "VIDEODL_") {
			os.Unsetenv(key)
		}
	}
	os.Setenv("HOME", root)
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	os.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	code := m.Run()
	os.RemoveAll(root)
	os.Exit(code)
}
