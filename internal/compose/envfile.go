package compose

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadEnvFile reads a .env file and returns a map of key-value pairs.
// It supports:
//   - KEY=VALUE lines
//   - Comments (lines starting with #)
//   - Blank lines (skipped)
//   - export KEY=VALUE prefix (stripped)
//   - Surrounding quotes on values (single and double, stripped)
//   - Inline comments after unquoted values
func LoadEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open env file %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	env := make(map[string]string)
	scanner := bufio.NewScanner(f)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip blank lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip "export " prefix.
		line = strings.TrimPrefix(line, "export ")

		// Split on first '='.
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		value = strings.TrimSpace(value)
		value = stripQuotes(value)

		env[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read env file %s: %w", path, err)
	}

	return env, nil
}

// parseEnvFileField parses the compose env_file field which can be a string or list of strings.
func parseEnvFileField(v any) ([]string, error) {
	switch ef := v.(type) {
	case string:
		return []string{ef}, nil
	case []any:
		var paths []string
		for _, item := range ef {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("invalid env_file element: %v", item)
			}
			paths = append(paths, s)
		}
		return paths, nil
	default:
		return nil, fmt.Errorf("invalid env_file type: %T", v)
	}
}

// stripQuotes removes matching surrounding quotes (single or double) from a value.
// It also strips inline comments from unquoted values.
func stripQuotes(s string) string {
	const minQuotedLen = 2
	if len(s) >= minQuotedLen {
		if (s[0] == '"' && s[len(s)-1] == '"') ||
			(s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}

	// Strip inline comments for unquoted values.
	if idx := strings.Index(s, " #"); idx != -1 {
		s = strings.TrimSpace(s[:idx])
	}

	return s
}
