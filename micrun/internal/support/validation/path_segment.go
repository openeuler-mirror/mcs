package validation

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrInvalidPathSegment reports an empty, padded, or multi-segment path value.
var ErrInvalidPathSegment = errors.New("invalid path segment")

// IsSinglePathSegment reports whether value is safe to use as one path segment.
func IsSinglePathSegment(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" &&
		trimmed == value &&
		value == filepath.Base(value) &&
		!strings.ContainsAny(value, `/\`) &&
		!strings.ContainsRune(value, '\x00') &&
		value != "." &&
		value != ".."
}

// NormalizeSinglePathSegment validates value and returns the safe segment.
func NormalizeSinglePathSegment(value string) (string, error) {
	if !IsSinglePathSegment(value) {
		return "", ErrInvalidPathSegment
	}
	return value, nil
}
