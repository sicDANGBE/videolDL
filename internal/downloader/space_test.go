package downloader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestReservationsAccountForOtherTransfersAndRelease(t *testing.T) {
	const gib = int64(1 << 30)
	b := newDiskBudget()
	free := int64(10 * gib)
	b.inspect = func(dir string) (diskInfo, error) {
		dev := uint64(1)
		if dir == "/other" {
			dev = 2
		}
		return diskInfo{device: dev, free: free}, nil
	}
	a, err := b.reserve("/same/a", 2*gib)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.size(6 * gib); err != nil {
		t.Fatal(err)
	}
	second, err := b.reserve("/same/b", 2*gib)
	if err != nil {
		t.Fatal(err)
	}
	defer second.release()
	if err := second.size(3 * gib); !errors.Is(err, ErrInsufficientSpace) {
		t.Fatalf("overcommit accepted: %v", err)
	}
	other, err := b.reserve("/other/c", 2*gib)
	if err != nil {
		t.Fatal(err)
	}
	defer other.release()
	if err := other.size(7 * gib); err != nil {
		t.Fatal("another filesystem shares reservation", err)
	}
	a.release()
	if err := second.size(3 * gib); err != nil {
		t.Fatal(err)
	}
	if err := second.size(math.MaxInt64); !errors.Is(err, ErrInsufficientSpace) {
		t.Fatalf("huge reservation: %v", err)
	}
}
func TestDiskGuardStopsNativeWritesAndExternalGrowth(t *testing.T) {
	const mib = int64(1 << 20)
	b := newDiskBudget()
	free := int64(256 * mib)
	b.inspect = func(string) (diskInfo, error) { return diskInfo{device: 1, free: free}, nil }
	r, err := b.reserve(filepath.Join(t.TempDir(), "out"), 128*mib)
	if err != nil {
		t.Fatal(err)
	}
	defer r.release()
	var buffer bytes.Buffer
	writer := r.writer(&buffer)
	if _, err := writer.Write(make([]byte, mib)); err != nil {
		t.Fatal(err)
	}
	free = 128 * mib
	if _, err := writer.Write([]byte("blocked")); !errors.Is(err, ErrInsufficientSpace) {
		t.Fatalf("disk floor ignored: %v", err)
	}
	if buffer.Len() != int(mib) {
		t.Fatal("write continued below disk floor")
	}
	file := filepath.Join(t.TempDir(), "external.part")
	if err := os.WriteFile(file, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	var size int64
	if err := r.external(file, &size); !errors.Is(err, ErrInsufficientSpace) {
		t.Fatalf("external disk floor ignored: %v", err)
	}
}
func TestOversizedHTTPDownloadFailsBeforePublishing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(int64(4<<30)))
		w.WriteHeader(200)
	}))
	defer server.Close()
	d := New(server.Client(), "")
	d.space.inspect = func(string) (diskInfo, error) { return diskInfo{device: 1, free: 3 << 30}, nil }
	output := filepath.Join(t.TempDir(), "large.mp4")
	req, err := NewRequest(server.URL, output, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Download(context.Background(), req); !errors.Is(err, ErrInsufficientSpace) {
		t.Fatalf("download: %v", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("published incomplete output: %v", err)
	}
	if len(d.space.active) != 0 {
		t.Fatal("reservation leaked")
	}
}
