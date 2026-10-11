package safety

import (
	"fmt"
	"strings"
)

// ValidateRuntimeModel permits explicit provider/model references, not aliases,
// URLs or configuration fragments. Empty preserves existing runtime defaults.
func ValidateRuntimeModel(runtimeID, model string) error {
	if model == "" {
		return nil
	}
	if strings.ToLower(strings.TrimSpace(runtimeID)) != "openclaw" {
		return fmt.Errorf("explicit runtime model selection is supported only for OpenClaw Gateway")
	}
	invalid := func() error {
		return fmt.Errorf("runtime model must be a provider/model reference of at most 255 characters")
	}
	if len(model) > 255 || !strings.Contains(model, "/") {
		return invalid()
	}
	for _, part := range strings.Split(model, "/") {
		if part == "" || part == "." || part == ".." {
			return invalid()
		}
		for _, c := range part {
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.:", c) {
				continue
			}
			return invalid()
		}
	}
	if strings.Contains(strings.SplitN(model, "/", 2)[0], ":") {
		return invalid()
	}
	return nil
}
