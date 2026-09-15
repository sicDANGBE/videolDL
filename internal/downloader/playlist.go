package downloader

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type variant struct {
	URI        string
	Bandwidth  uint64
	Height     int
	AudioGroup string
}

type segment struct {
	URI string
}

type audioTrack struct {
	URI, Group string
	Default    bool
}

type playlist struct {
	audio          []audioTrack
	requiresFFmpeg bool
	variants       []variant
	segments       []segment
	mapURI         string
	encrypted      bool
}

func parsePlaylist(input io.Reader) (playlist, error) {
	var result playlist
	if input == nil {
		return playlist{}, fmt.Errorf("nil playlist: %w", ErrInvalidPlaylist)
	}

	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var pendingVariant *variant
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if pendingVariant != nil && !strings.HasPrefix(line, "#") {
			pendingVariant.URI = line
			result.variants = append(result.variants, *pendingVariant)
			pendingVariant = nil
			continue
		}
		switch {
		case strings.HasPrefix(line, "#EXT-X-STREAM-INF:"):
			attrs, err := parseAttributes(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"))
			if err != nil {
				return playlist{}, fmt.Errorf("parse variant: %w", err)
			}
			bandwidth, err := parseUintAttribute(attrs, "BANDWIDTH")
			if err != nil {
				return playlist{}, fmt.Errorf("parse variant bandwidth: %w", err)
			}
			height := 0
			if resolution := attrs["RESOLUTION"]; resolution != "" {
				_, value, ok := strings.Cut(resolution, "x")
				if !ok {
					return playlist{}, ErrInvalidPlaylist
				}
				height, err = strconv.Atoi(value)
				if err != nil || height <= 0 {
					return playlist{}, ErrInvalidPlaylist
				}
			}
			pendingVariant = &variant{Bandwidth: bandwidth, Height: height, AudioGroup: attrs["AUDIO"]}
		case strings.HasPrefix(line, "#EXT-X-MEDIA:"):
			attrs, err := parseAttributes(strings.TrimPrefix(line, "#EXT-X-MEDIA:"))
			if err != nil {
				return playlist{}, err
			}
			if attrs["TYPE"] == "AUDIO" && attrs["URI"] != "" {
				result.audio = append(result.audio, audioTrack{URI: attrs["URI"], Group: attrs["GROUP-ID"], Default: attrs["DEFAULT"] == "YES"})
			}
		case strings.HasPrefix(line, "#EXT-X-BYTERANGE:"), line == "#EXT-X-DISCONTINUITY":
			result.requiresFFmpeg = true
		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			attrs, err := parseAttributes(strings.TrimPrefix(line, "#EXT-X-KEY:"))
			if err != nil {
				return playlist{}, fmt.Errorf("parse encryption key: %w", err)
			}
			method := strings.ToUpper(attrs["METHOD"])
			if method != "" && method != "NONE" {
				result.encrypted = true
			}
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			attrs, err := parseAttributes(strings.TrimPrefix(line, "#EXT-X-MAP:"))
			if err != nil {
				return playlist{}, fmt.Errorf("parse initialization map: %w", err)
			}
			result.mapURI = attrs["URI"]
		case !strings.HasPrefix(line, "#"):
			result.segments = append(result.segments, segment{URI: line})
		}
	}
	if err := scanner.Err(); err != nil {
		return playlist{}, fmt.Errorf("read playlist: %w", err)
	}
	if pendingVariant != nil || (len(result.variants) == 0 && len(result.segments) == 0) {
		return playlist{}, fmt.Errorf("playlist contains no complete media entries: %w", ErrInvalidPlaylist)
	}
	if result.encrypted {
		return playlist{}, ErrEncryptedHLS
	}
	return result, nil
}

func parseAttributes(raw string) (map[string]string, error) {
	attributes := make(map[string]string)
	start := 0
	inQuotes := false
	for i, char := range raw {
		switch char {
		case '"':
			inQuotes = !inQuotes
		case ',':
			if !inQuotes {
				if err := addAttribute(attributes, raw[start:i]); err != nil {
					return nil, err
				}
				start = i + 1
			}
		}
	}
	if inQuotes {
		return nil, fmt.Errorf("unterminated quoted attribute: %w", ErrInvalidPlaylist)
	}
	if err := addAttribute(attributes, raw[start:]); err != nil {
		return nil, err
	}
	return attributes, nil
}

func addAttribute(attributes map[string]string, raw string) error {
	key, value, found := strings.Cut(raw, "=")
	if !found || strings.TrimSpace(key) == "" {
		return fmt.Errorf("malformed attribute %q: %w", raw, ErrInvalidPlaylist)
	}
	value = strings.Trim(strings.TrimSpace(value), "\"")
	attributes[strings.ToUpper(strings.TrimSpace(key))] = value
	return nil
}

func parseUintAttribute(attributes map[string]string, key string) (uint64, error) {
	value, found := attributes[key]
	if !found {
		return 0, fmt.Errorf("missing %s attribute: %w", key, ErrInvalidPlaylist)
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s value: %w", key, err)
	}
	return parsed, nil
}

func highestVariant(variants []variant) (variant, error) {
	if len(variants) == 0 {
		return variant{}, fmt.Errorf("no HLS variants: %w", ErrInvalidPlaylist)
	}
	selected := variants[0]
	for _, candidate := range variants[1:] {
		if candidate.Bandwidth > selected.Bandwidth {
			selected = candidate
		}
	}
	return selected, nil
}

func selectVariant(variants []variant, maxHeight int) (variant, error) {
	if maxHeight == 0 {
		return highestVariant(variants)
	}
	var eligible []variant
	for _, candidate := range variants {
		if candidate.Height > 0 && candidate.Height <= maxHeight {
			eligible = append(eligible, candidate)
		}
	}
	if len(eligible) == 0 {
		return variant{}, fmt.Errorf("no HLS variant with a known height at or below %dp", maxHeight)
	}
	return highestVariant(eligible)
}
func selectAudio(tracks []audioTrack, group string) string {
	var uri string
	for _, track := range tracks {
		if track.Group == group {
			if uri == "" || track.Default {
				uri = track.URI
			}
			if track.Default {
				break
			}
		}
	}
	return uri
}
