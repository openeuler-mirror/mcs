package fs

import "testing"

func TestValidContainerID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr string
	}{
		{
			name:    "empty",
			id:      "",
			wantErr: "validation error: container ID: cannot be empty",
		},
		{
			name:    "too long",
			id:      "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmno",
			wantErr: "validation error: container ID: exceeds mica limit (66 characters)",
		},
		{
			name:    "invalid format",
			id:      "-invalid",
			wantErr: "validation error: container ID: invalid format",
		},
		{
			name: "valid",
			id:   "container_01.test",
		},
		{
			name: "single character",
			id:   "a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidContainerID(tt.id)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidContainerID returned unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("ValidContainerID returned nil error")
			}
			if got := err.Error(); got != tt.wantErr {
				t.Fatalf("error = %q, want %q", got, tt.wantErr)
			}
		})
	}
}
