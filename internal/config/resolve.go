package config

import (
	"fmt"
	"os"
	"strings"
)

// resolveEnvVar resolves a value that may be a $VAR_NAME reference.
func resolveEnvVar(value string) (string, bool) {
	if strings.HasPrefix(value, "$") {
		envName := value[1:]
		resolved := os.Getenv(envName)
		return resolved, resolved != ""
	}
	return value, value != ""
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home + path[1:]
	}
	return path
}

// parseStateList parses a list of strings or a comma-separated string into
// a normalized (trimmed, lowercased) string slice.
func parseStateList(raw any, defaultStates []string) []string {
	if raw == nil {
		return defaultStates
	}

	switch v := raw.(type) {
	case string:
		parts := strings.Split(v, ",")
		result := make([]string, 0, len(parts))
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				result = append(result, strings.ToLower(trimmed))
			}
		}
		return result
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				trimmed := strings.TrimSpace(s)
				if trimmed != "" {
					result = append(result, strings.ToLower(trimmed))
				}
			}
		}
		return result
	}
	return defaultStates
}

// getNestedString extracts a string from a nested map path like "tracker.kind".
func getNestedString(m map[string]any, keys ...string) string {
	current := any(m)
	for _, k := range keys {
		cm, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = cm[k]
	}
	if s, ok := current.(string); ok {
		return s
	}
	return ""
}

// getNestedInt extracts an int from a nested map path, coercing from string or float.
func getNestedInt(m map[string]any, defaultVal int, keys ...string) int {
	current := any(m)
	for _, k := range keys {
		cm, ok := current.(map[string]any)
		if !ok {
			return defaultVal
		}
		current = cm[k]
	}
	if current == nil {
		return defaultVal
	}
	switch v := current.(type) {
	case int:
		return v
	case float64:
		return int(v)
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return defaultVal
}

// getNestedMap extracts a map from a nested map path.
func getNestedMap(m map[string]any, keys ...string) map[string]any {
	current := any(m)
	for _, k := range keys {
		cm, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = cm[k]
	}
	if cm, ok := current.(map[string]any); ok {
		return cm
	}
	return nil
}
