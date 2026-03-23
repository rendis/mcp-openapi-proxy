package server

import (
	"fmt"
	"strings"
)

func toolName(prefix, method, path string) string {
	sanitized := sanitizePath(path)
	if sanitized == "" {
		return strings.ToLower(fmt.Sprintf("%s_%s", prefix, method))
	}
	return strings.ToLower(fmt.Sprintf("%s_%s_%s", prefix, method, sanitized))
}

func sanitizePath(path string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"-", "_",
		"{", "_",
		"}", "_",
		".", "_",
	)
	s := replacer.Replace(path)
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}
	return strings.Trim(strings.ToLower(s), "_")
}
