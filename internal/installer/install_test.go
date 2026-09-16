package installer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"video-downloader/internal/queue"
)

var testBinary, testManual string

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "videodl-install-tests-")
	if err != nil {
		panic(err)
	}
	testBinary = filepath.Join(root, "videodl")
	testManual, err = filepath.Abs("../cli/assets/videodl.1")
	if err != nil {
		panic(err)
	}
	command := exec.Command("go", "build", "-o", testBinary, "../../cmd/videodl")
	if data, err := command.CombinedOutput(); err != nil {
		fmt.Fprintln(os.Stderr, string(data), err)
		os.RemoveAll(root)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(root)
	os.Exit(code)
}

type fixture struct {
	root, prefix, exe, config, pid, state, destination string
	env                                                []string
	out                                                bytes.Buffer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{root: root, prefix: filepath.Join(root, "local"), destination: filepath.Join(root, "media")}
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "VIDEODL_") && !strings.HasPrefix(item, "HOME=") && !strings.HasPrefix(item, "XDG_CONFIG_HOME=") && !strings.HasPrefix(item, "XDG_STATE_HOME=") {
			f.env = append(f.env, item)
		}
	}
	f.env = append(f.env, "HOME="+root, "XDG_CONFIG_HOME="+filepath.Join(root, "config"), "XDG_STATE_HOME="+filepath.Join(root, "state"))
	f.exe = filepath.Join(f.prefix, "bin/videodl")
	f.config = filepath.Join(root, "config/videodl/config.json")
	f.pid = filepath.Join(root, "state/videodl/daemon.pid")
	f.state = filepath.Join(root, "state/videodl/queue.json")
	if err := Install(context.Background(), f.options()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "Installation terminée") {
		t.Fatal(f.out.String())
	}
	t.Cleanup(func() {
		services, err := servicesAt(f.exe)
		if err != nil {
			return
		}
		for _, s := range services {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = stopService(ctx, s)
			cancel()
		}
	})
	f.run(t, "setup", "--destination", f.destination)
	return f
}
func (f *fixture) options() Options {
	return Options{Prefix: f.prefix, Binary: testBinary, Manual: testManual, Out: &f.out}
}
func (f *fixture) run(t *testing.T, args ...string) string {
	t.Helper()
	command := exec.Command(f.exe, args...)
	command.Env = f.env
	command.Dir = f.root
	data, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("command %v: %v: %s", args, err, data)
	}
	return string(data)
}
func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(15 * time.Millisecond)
	}
}
func (f *fixture) start(t *testing.T) int {
	t.Helper()
	f.run(t, "daemon", "start")
	eventually(t, func() bool { return queueBusy(f.state) })
	pid, err := readPID(f.pid)
	if err != nil {
		t.Fatal(err)
	}
	return pid
}
func queueBusy(path string) bool {
	store, err := queue.NewStore(path)
	if err != nil {
		return false
	}
	release, err := store.LockSupervisor()
	if release != nil {
		release()
	}
	return errors.Is(err, queue.ErrSupervisorRunning)
}
func TestInstallAndUpdatePreserveIdleProfile(t *testing.T) {
	f := newFixture(t)
	before, err := os.ReadFile(f.config)
	if err != nil {
		t.Fatal(err)
	}
	f.out.Reset()
	if err := Install(context.Background(), f.options()); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(f.config)
	if !bytes.Equal(before, after) {
		t.Fatal("configuration overwritten")
	}
	if _, err := os.Stat(f.pid); !os.IsNotExist(err) {
		t.Fatal("idle service started")
	}
	if !strings.Contains(f.out.String(), "Mise à jour terminée") || strings.Contains(f.out.String(), "setup --destination") {
		t.Fatal(f.out.String())
	}
	for _, relative := range []string{"share/man/man1/videodl.1", "share/bash-completion/completions/videodl", "share/zsh/site-functions/_videodl", "share/fish/vendor_completions.d/videodl.fish"} {
		if _, err := os.Stat(filepath.Join(f.prefix, relative)); err != nil {
			t.Fatal(err)
		}
	}
}
func TestUpdateStopsAndResumesActiveHTTPDownload(t *testing.T) {
	f := newFixture(t)
	payload := bytes.Repeat([]byte("video-data"), 32768)
	var requests atomic.Int32
	var resumed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"stable"`)
		offset := 0
		if value := r.Header.Get("Range"); value != "" {
			fmt.Sscanf(value, "bytes=%d-", &offset)
			resumed.Store(offset > 0)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, len(payload)-1, len(payload)))
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)-offset))
		if offset > 0 {
			w.WriteHeader(http.StatusPartialContent)
		}
		if requests.Add(1) == 1 {
			w.Write(payload[:65536])
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		w.Write(payload[offset:])
	}))
	defer server.Close()
	f.run(t, "add", "--name", "test.mp4", server.URL+"/file.mp4")
	oldPID := f.start(t)
	defer func() {
		services, _ := servicesAt(f.exe)
		for _, s := range services {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			stopService(ctx, s)
			cancel()
		}
	}()
	eventually(t, func() bool {
		entries, _ := filepath.Glob(filepath.Join(f.destination, ".videodl", "*.part"))
		for _, path := range entries {
			info, err := os.Stat(path)
			if err == nil && info.Size() > 0 {
				return true
			}
		}
		return false
	})
	before, _ := os.ReadFile(f.config)
	if err := Install(context.Background(), f.options()); err != nil {
		t.Fatal(err)
	}
	newPID, err := readPID(f.pid)
	if err != nil || newPID == oldPID {
		t.Fatalf("service not restarted: %d -> %d (%v)", oldPID, newPID, err)
	}
	eventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join(f.destination, "test.mp4"))
		return err == nil && bytes.Equal(data, payload)
	})
	if !resumed.Load() {
		t.Fatal("HTTP partial was not resumed")
	}
	eventually(t, func() bool {
		jobs, err := queue.Snapshot(f.state)
		return err == nil && len(jobs) == 1 && jobs[0].Status == queue.StatusCompleted
	})
	after, _ := os.ReadFile(f.config)
	if !bytes.Equal(before, after) {
		t.Fatal("configuration overwritten")
	}
}
func TestUpdateFindsDaemonAfterPreviousBinaryReplacement(t *testing.T) {
	f := newFixture(t)
	oldPID := f.start(t)
	data, _ := os.ReadFile(testBinary)
	replacement := f.exe + ".replacement"
	if err := os.WriteFile(replacement, data, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, f.exe); err != nil {
		t.Fatal(err)
	}
	link, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", oldPID))
	if err != nil || !strings.HasSuffix(link, " (deleted)") {
		t.Fatalf("old inode not replaced: %s %v", link, err)
	}
	if err := Install(context.Background(), f.options()); err != nil {
		t.Fatal(err)
	}
	pid, err := readPID(f.pid)
	if err != nil || pid == oldPID {
		t.Fatal("old service not restarted", err)
	}
}
func TestPreparationFailureKeepsRunningService(t *testing.T) {
	f := newFixture(t)
	pid := f.start(t)
	before, _ := os.ReadFile(f.exe)
	options := f.options()
	options.Manual = filepath.Join(f.root, "missing-manual")
	if err := Install(context.Background(), options); err == nil {
		t.Fatal("missing manual accepted")
	}
	after, _ := os.ReadFile(f.exe)
	if !bytes.Equal(before, after) {
		t.Fatal("binary changed during preparation failure")
	}
	current, _ := readPID(f.pid)
	if current != pid || !queueBusy(f.state) {
		t.Fatal("service interrupted during preparation")
	}
}
func TestRenameFailureRestoresBinaryAndRestartsOldService(t *testing.T) {
	f := newFixture(t)
	oldPID := f.start(t)
	before, _ := os.ReadFile(f.exe)
	options := f.options()
	calls := 0
	options.replace = func(from, to string) error {
		calls++
		if calls == 2 {
			return errors.New("injected rename failure")
		}
		return os.Rename(from, to)
	}
	if err := Install(context.Background(), options); err == nil {
		t.Fatal("rename failure ignored")
	}
	after, _ := os.ReadFile(f.exe)
	if !bytes.Equal(before, after) {
		t.Fatal("binary not restored")
	}
	pid, err := readPID(f.pid)
	if err != nil || pid == oldPID || !queueBusy(f.state) {
		t.Fatal("previous service not relaunched", err)
	}
}
func TestStartupFailureRestoresPreviousVersion(t *testing.T) {
	f := newFixture(t)
	f.start(t)
	before, _ := os.ReadFile(f.exe)
	fake := filepath.Join(f.root, "broken-version")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nif [ \"$1\" = completion ]; then printf '# completion\\n'; exit 0; fi\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	options := f.options()
	options.Binary = fake
	if err := Install(context.Background(), options); err == nil {
		t.Fatal("broken daemon accepted")
	}
	after, _ := os.ReadFile(f.exe)
	if !bytes.Equal(before, after) {
		t.Fatal("old executable not restored")
	}
	if !queueBusy(f.state) {
		t.Fatal("previous service not relaunched")
	}
}
func TestForegroundWorkerPreventsReplacement(t *testing.T) {
	f := newFixture(t)
	before, _ := os.ReadFile(f.exe)
	command := exec.Command(f.exe, "worker", "--watch")
	command.Env = f.env
	command.Dir = f.root
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { command.Process.Signal(syscall.SIGTERM); command.Wait() }()
	eventually(t, func() bool { return queueBusy(f.state) })
	err := Install(context.Background(), f.options())
	if err == nil || !strings.Contains(err.Error(), "premier plan") {
		t.Fatal("foreground worker was not detected", err)
	}
	after, _ := os.ReadFile(f.exe)
	if !bytes.Equal(before, after) {
		t.Fatal("foreground executable replaced")
	}
	if !queueBusy(f.state) {
		t.Fatal("foreground worker interrupted")
	}
}
func TestConcurrentInstallIsRejected(t *testing.T) {
	f := newFixture(t)
	unlock, err := lockInstall(f.prefix)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := Install(context.Background(), f.options()); err == nil || !strings.Contains(err.Error(), "autre installation") {
		t.Fatal("parallel installation accepted", err)
	}
}

func TestCancellationRestoresFilesAndService(t *testing.T) {
	f := newFixture(t)
	oldPID := f.start(t)
	before, _ := os.ReadFile(f.exe)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	options := f.options()
	options.replace = func(from, to string) error { err := os.Rename(from, to); cancel(); return err }
	if err := Install(ctx, options); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation not reported", err)
	}
	after, _ := os.ReadFile(f.exe)
	if !bytes.Equal(before, after) {
		t.Fatal("binary not restored after cancellation")
	}
	pid, err := readPID(f.pid)
	if err != nil || pid == oldPID || !queueBusy(f.state) {
		t.Fatal("previous service not restored", err)
	}
}
func TestUpdateRestartsMultipleProfiles(t *testing.T) {
	f := newFixture(t)
	first := f.start(t)
	other := &fixture{root: filepath.Join(f.root, "other"), exe: f.exe, prefix: f.prefix, destination: filepath.Join(f.root, "other/media")}
	if err := os.MkdirAll(other.root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, item := range f.env {
		if !strings.HasPrefix(item, "XDG_STATE_HOME=") && !strings.HasPrefix(item, "XDG_CONFIG_HOME=") && !strings.HasPrefix(item, "VIDEODL_CONFIG=") {
			other.env = append(other.env, item)
		}
	}
	other.config = filepath.Join(other.root, "profile.json")
	other.pid = filepath.Join(other.root, "state/videodl/daemon.pid")
	other.state = filepath.Join(other.root, "state/videodl/queue.json")
	other.env = append(other.env, "VIDEODL_CONFIG="+other.config, "XDG_STATE_HOME="+filepath.Join(other.root, "state"), "XDG_CONFIG_HOME="+filepath.Join(other.root, "config"))
	other.run(t, "setup", "--config", other.config, "--destination", other.destination)
	second := other.start(t)
	before, _ := os.ReadFile(other.config)
	if err := Install(context.Background(), f.options()); err != nil {
		t.Fatal(err)
	}
	a, _ := readPID(f.pid)
	b, _ := readPID(other.pid)
	if a == first || b == second || !queueBusy(f.state) || !queueBusy(other.state) {
		t.Fatal("both profiles not relaunched")
	}
	after, _ := os.ReadFile(other.config)
	if !bytes.Equal(before, after) {
		t.Fatal("custom profile modified")
	}
}
