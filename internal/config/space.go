package config

import (
	"math"
	"strconv"
	"strings"
)

// ParseSpace accepts a nonnegative integer in bytes, KiB, MiB, GiB or TiB.
func ParseSpace(value string) (int64, error) {
	text := strings.TrimSpace(value)
	multiplier := int64(1)
	for _, unit := range []struct {
		name string
		size int64
	}{{"KiB", 1 << 10}, {"MiB", 1 << 20}, {"GiB", 1 << 30}, {"TiB", 1 << 40}, {"B", 1}} {
		if strings.HasSuffix(text, unit.name) {
			multiplier = unit.size
			text = strings.TrimSpace(strings.TrimSuffix(text, unit.name))
			break
		}
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil || n < 0 || n > math.MaxInt64/multiplier {
		return 0, &ValidationError{Field: "min_free_space", Rule: "entier positif ou nul en octets, KiB, MiB, GiB ou TiB"}
	}
	return n * multiplier, nil
}
func FormatSpace(n int64) string {
	for _, unit := range []struct {
		name string
		size int64
	}{{"TiB", 1 << 40}, {"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10}} {
		if n > 0 && n%unit.size == 0 {
			return strconv.FormatInt(n/unit.size, 10) + unit.name
		}
	}
	return strconv.FormatInt(n, 10)
}
