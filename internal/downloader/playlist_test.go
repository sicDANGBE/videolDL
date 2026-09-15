package downloader

import (
	"errors"
	"strings"
	"testing"
)

func TestParsePlaylist_selects_master_variants(t *testing.T) {
	input := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=100000,RESOLUTION="640x360"
low/index.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION="1280x720"
high/index.m3u8
`
	parsed, err := parsePlaylist(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parsePlaylist returned error: %v", err)
	}
	selected, err := highestVariant(parsed.variants)
	if err != nil {
		t.Fatalf("highestVariant returned error: %v", err)
	}
	if selected.URI != "high/index.m3u8" {
		t.Fatalf("selected URI = %q", selected.URI)
	}
}

func TestParsePlaylist_reads_media_segments_and_map(t *testing.T) {
	input := `#EXTM3U
#EXT-X-MAP:URI="init.mp4"
#EXTINF:4,
segments/one.m4s
#EXTINF:4,
segments/two.m4s
`
	parsed, err := parsePlaylist(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parsePlaylist returned error: %v", err)
	}
	if parsed.mapURI != "init.mp4" || len(parsed.segments) != 2 {
		t.Fatalf("playlist = %#v", parsed)
	}
}

func TestParsePlaylist_rejects_encrypted_media(t *testing.T) {
	input := `#EXTM3U
#EXT-X-KEY:METHOD=AES-128,URI="key.bin"
#EXTINF:4,
segment.ts
`
	_, err := parsePlaylist(strings.NewReader(input))
	if !errors.Is(err, ErrEncryptedHLS) {
		t.Fatalf("expected ErrEncryptedHLS, got %v", err)
	}
}
