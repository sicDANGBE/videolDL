package downloader

import (
	"fmt"
	"net/url"
	"strings"
)

type URL struct {
	value string
}

func ParseURL(raw string) (URL, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil {
		return URL{}, fmt.Errorf("parse URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return URL{}, fmt.Errorf("%q: %w", raw, ErrInvalidURL)
	}
	return URL{value: parsed.String()}, nil
}

func (u URL) String() string {
	return u.value
}

type Request struct {
	options    Overrides
	source     URL
	outputPath string
	useFFmpeg  bool
}

func NewRequest(source, outputPath string, useFFmpeg bool) (Request, error) {
	parsed, err := ParseURL(source)
	if err != nil {
		return Request{}, err
	}
	if strings.TrimSpace(outputPath) == "" {
		return Request{}, fmt.Errorf("output path is empty: %w", ErrInvalidOutput)
	}
	return Request{source: parsed, outputPath: outputPath, useFFmpeg: useFFmpeg}, nil
}

func (r Request) Source() URL {
	return r.source
}

func (r Request) OutputPath() string {
	return r.outputPath
}

func (r Request) UseFFmpeg() bool {
	return r.useFFmpeg
}

func (r Request) WithOptions(options Overrides) Request { r.options = options; return r }
func (r Request) Options() Overrides                    { return r.options }
