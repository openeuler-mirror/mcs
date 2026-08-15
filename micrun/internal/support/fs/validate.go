package fs

import (
	"fmt"
	"regexp"
)

// maxClientIDLength is the longest id micad can register: it forces
// name[MAX_NAME_LEN-1] = '\0', so a full-length (66) id is truncated to 65
// and its per-client control socket path would never match. Keep this in
// sync with libmica.validateClientID.
const maxClientIDLength = 65

var cidPattern = regexp.MustCompile("^[a-zA-Z0-9][a-zA-Z0-9_.-]*$")

func ValidContainerID(id string) error {
	if id == "" {
		return &ValidationError{Field: "container ID", Issue: "cannot be empty"}
	}

	if len(id) > maxClientIDLength {
		return &ValidationError{Field: "container ID", Issue: fmt.Sprintf("exceeds mica limit (%d characters)", maxClientIDLength)}
	}

	if !cidPattern.MatchString(id) {
		return &ValidationError{Field: "container ID", Issue: "invalid format"}
	}
	return nil
}

type ValidationError struct {
	Field string
	Issue string
}

func (e *ValidationError) Error() string {
	return "validation error: " + e.Field + ": " + e.Issue
}
