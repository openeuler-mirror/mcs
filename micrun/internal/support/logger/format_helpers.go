package log

import "strconv"

// needsQuoting checks if a string needs quoting in containerd-style log fields.
func needsQuoting(s string) bool {
	for _, c := range s {
		if c <= ' ' || c == '"' || c == '\\' {
			return true
		}
	}
	return false
}

func formatContainerdValue(s string) string {
	if needsQuoting(s) {
		return strconv.Quote(s)
	}
	return s
}
