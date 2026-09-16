package config

import (
	"testing"
	"time"
)

func TestParseSpaceBounds(t *testing.T) {
	for text, want := range map[string]int64{"0": 0, "4096": 4096, "2GiB": 2 << 30, "512MiB": 512 << 20, " 1 TiB ": 1 << 40} {
		got, err := ParseSpace(text)
		if err != nil || got != want {
			t.Fatalf("%q: %d %v", text, got, err)
		}
	}
	for _, text := range []string{"", "-1", "-2GiB", "1.5GiB", "many", "9223372036854775807GiB"} {
		if _, err := ParseSpace(text); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
}
func TestTransferEnvironmentOverridesAreApplied(t *testing.T) {
	c, err := Load(LoadOptions{HomeDir: t.TempDir(), WorkingDir: t.TempDir(), Env: []string{"VIDEODL_RESUME=false", "VIDEODL_IDLE_TIMEOUT=15s", "VIDEODL_MAX_HEIGHT=720", "VIDEODL_MIN_FREE_SPACE=512MiB"}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Resume || c.IdleTimeout != 15*time.Second || c.MaxHeight != 720 || c.MinFreeSpace != 512<<20 {
		t.Fatal("transfer environment ignored")
	}
}
