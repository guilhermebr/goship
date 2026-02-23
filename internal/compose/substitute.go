package compose

import (
	"os"
	"regexp"
	"strings"
)

// varPattern matches ${VAR}, ${VAR:-default}, and $VAR patterns.
var varPattern = regexp.MustCompile(`\$\{([^}]+)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// substituteVars resolves variable references in a map of environment values.
// It supports ${VAR}, $VAR, and ${VAR:-default} syntax.
// The context map provides variable values; host environment is used as fallback.
// Unresolved variables become empty strings (matches Docker Compose behavior).
func substituteVars(env map[string]string, context map[string]string) map[string]string {
	result := make(map[string]string, len(env))
	for k, v := range env {
		result[k] = resolveValue(v, context)
	}
	return result
}

// resolveValue replaces variable references in a single string value.
func resolveValue(s string, context map[string]string) string {
	return varPattern.ReplaceAllStringFunc(s, func(match string) string {
		// ${VAR} or ${VAR:-default} form.
		if strings.HasPrefix(match, "${") {
			inner := match[2 : len(match)-1] // strip ${ and }

			// Check for ${VAR:-default} syntax.
			if name, defaultVal, ok := strings.Cut(inner, ":-"); ok {
				if val, found := lookupVar(name, context); found && val != "" {
					return val
				}
				return defaultVal
			}

			// Plain ${VAR}.
			if val, found := lookupVar(inner, context); found {
				return val
			}
			return ""
		}

		// $VAR form.
		name := match[1:] // strip $
		if val, found := lookupVar(name, context); found {
			return val
		}
		return ""
	})
}

// lookupVar looks up a variable name in the context map, then falls back to host environment.
func lookupVar(name string, context map[string]string) (string, bool) {
	if val, ok := context[name]; ok {
		return val, true
	}
	if val, ok := os.LookupEnv(name); ok {
		return val, true
	}
	return "", false
}
