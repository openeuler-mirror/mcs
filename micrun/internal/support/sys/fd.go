package sys

import "micrun/internal/support/validation"

// FDOf returns the file descriptor exposed by value when it implements Fd.
func FDOf(value any) (int, bool) {
	if value == nil || validation.IsNil(value) {
		return 0, false
	}
	fdObj, ok := value.(interface{ Fd() uintptr })
	if !ok {
		return 0, false
	}
	return int(fdObj.Fd()), true
}
