package shim

import "testing"

func TestNormalizeShimNamespace(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{
			name: "valid",
			raw:  "default",
			want: "default",
		},
		{
			name:    "empty",
			raw:     "",
			wantErr: true,
		},
		{
			name:    "padded",
			raw:     " default",
			wantErr: true,
		},
		{
			name:    "path separator",
			raw:     "tenant/default",
			wantErr: true,
		},
		{
			name:    "backslash separator",
			raw:     `tenant\default`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeShimNamespace(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeShimNamespace returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("normalizeShimNamespace(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
