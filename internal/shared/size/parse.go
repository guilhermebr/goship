package size

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ParseSizeMB parses a human-readable size string into megabytes.
// Accepts: "512" or "512M"/"512m" (megabytes), "8G"/"8g" (gigabytes).
// Plain integers without a suffix are treated as megabytes for backward compatibility.
func ParseSizeMB(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return 0, errors.New("empty size value")
	}

	const (
		bytesPerGB = 1024
	)

	suffix := s[len(s)-1]

	switch suffix {
	case 'm', 'M':
		val, err := strconv.ParseInt(s[:len(s)-1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid size %q: %w", s, err)
		}
		if val < 0 {
			return 0, fmt.Errorf("negative size not allowed: %s", s)
		}
		return val, nil

	case 'g', 'G':
		val, err := strconv.ParseInt(s[:len(s)-1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid size %q: %w", s, err)
		}
		if val < 0 {
			return 0, fmt.Errorf("negative size not allowed: %s", s)
		}
		return val * bytesPerGB, nil

	default:
		// No suffix: treat entire string as megabytes (backward compat).
		val, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid size %q: expected a number with optional M or G suffix (e.g., 512M, 8G)", s)
		}
		if val < 0 {
			return 0, fmt.Errorf("negative size not allowed: %s", s)
		}
		return val, nil
	}
}
