//go:build test
// +build test

package libmica

// OverrideClientNotExistForTest temporarily swaps the client existence checker.
func OverrideClientNotExistForTest(fn func(string) bool) func() {
	prev := clientNotExistFn
	if fn == nil {
		clientNotExistFn = defaultClientNotExist
	} else {
		clientNotExistFn = fn
	}
	return func() {
		clientNotExistFn = prev
	}
}
