package attach

import (
	"testing"

	"micrun/internal/ports"
)

func TestShouldRestartAttachForResize(t *testing.T) {
	tests := []struct {
		name         string
		manager      ports.IOManager
		terminal     bool
		isRealAttach bool
		want         bool
	}{
		{
			name:         "restart when manager is missing",
			manager:      nil,
			terminal:     true,
			isRealAttach: true,
			want:         true,
		},
		{
			name:         "restart when manager is missing in non-tty mode",
			manager:      nil,
			terminal:     false,
			isRealAttach: false,
			want:         true,
		},
		{
			name:         "keep manager for running terminal real attach",
			manager:      &fakeIOManager{isRunning: true},
			terminal:     true,
			isRealAttach: true,
			want:         false,
		},
		{
			name:         "keep manager for running terminal detached attach",
			manager:      &fakeIOManager{isRunning: true},
			terminal:     true,
			isRealAttach: false,
			want:         false,
		},
		{
			name:         "restart for non-tty real attach",
			manager:      &fakeIOManager{isRunning: true},
			terminal:     false,
			isRealAttach: true,
			want:         true,
		},
		{
			name:         "keep manager for non-tty detached attach",
			manager:      &fakeIOManager{isRunning: true},
			terminal:     false,
			isRealAttach: false,
			want:         false,
		},
		{
			name:         "restart when manager not running",
			manager:      &fakeIOManager{isRunning: false},
			terminal:     true,
			isRealAttach: true,
			want:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRestartAttachForResize(tt.manager, tt.terminal, tt.isRealAttach)
			if got != tt.want {
				t.Fatalf("shouldRestartAttachForResize(%v, %v, %v) = %v, want %v",
					tt.manager != nil, tt.terminal, tt.isRealAttach, got, tt.want)
			}
		})
	}
}
