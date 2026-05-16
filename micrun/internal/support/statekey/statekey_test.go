package statekey

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestNormalizeAcceptsNestedRelativeKey(t *testing.T) {
	got, err := Normalize("runtime/container")
	if err != nil {
		t.Fatalf("Normalize returned error: %v", err)
	}
	if got != "runtime/container" {
		t.Fatalf("Normalize = %q, want runtime/container", got)
	}
}

func TestNormalizeConvertsBackslashSeparators(t *testing.T) {
	got, err := Normalize(`runtime\container`)
	if err != nil {
		t.Fatalf("Normalize returned error: %v", err)
	}
	want := filepath.Join("runtime", "container")
	if got != want {
		t.Fatalf("Normalize = %q, want %q", got, want)
	}
}

func TestNormalizeRejectsUnsafeKeys(t *testing.T) {
	for _, input := range []string{"", " \t ", " runtime", "runtime ", "/abs", "../escape", "a/../b", `a\..\b`, "a//b", "a/b/", "runtime\x00container"} {
		t.Run(input, func(t *testing.T) {
			if _, err := Normalize(input); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Normalize(%q) error = %v, want ErrInvalid", input, err)
			}
		})
	}
}
