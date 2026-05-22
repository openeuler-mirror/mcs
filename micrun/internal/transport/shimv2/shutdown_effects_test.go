package shim

import (
	"errors"
	"os"
	"testing"
)

func TestShouldWarnShutdownSocketRemoval(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "socket already removed", err: os.ErrNotExist, want: false},
		{name: "wrapped socket already removed", err: &os.PathError{Op: "remove", Path: "/run/containerd/s/demo", Err: os.ErrNotExist}, want: false},
		{name: "unexpected remove failure", err: errors.New("permission denied"), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldWarnShutdownSocketRemoval(tt.err); got != tt.want {
				t.Fatalf("shouldWarnShutdownSocketRemoval() = %v, want %v", got, tt.want)
			}
		})
	}
}
