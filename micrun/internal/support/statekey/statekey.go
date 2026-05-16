package statekey

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrInvalid reports an unsafe or empty runtime state key.
var ErrInvalid = errors.New("invalid state key")

// Normalize validates a relative state key and returns its canonical form.
func Normalize(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed != value {
		return "", ErrInvalid
	}
	if strings.ContainsRune(value, '\x00') {
		return "", ErrInvalid
	}
	if filepath.IsAbs(value) {
		return "", ErrInvalid
	}

	canonicalSeparators := strings.ReplaceAll(value, "\\", "/")
	for _, segment := range strings.Split(canonicalSeparators, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", ErrInvalid
		}
	}
	value = strings.ReplaceAll(canonicalSeparators, "/", string(filepath.Separator))
	clean := filepath.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", ErrInvalid
	}
	return clean, nil
}
