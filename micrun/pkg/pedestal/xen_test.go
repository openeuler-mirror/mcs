//go:build test
// +build test

package pedestal

import "testing"

func TestShareToWeight(t *testing.T) {
	tests := []struct {
		name   string
		shares uint64
		want   uint32
	}{
		{
			name:   "zero shares uses default",
			shares: 0,
			want:   DefaultXenWeight,
		},
		{
			name:   "standard cgroup shares",
			shares: 1024,
			want:   256,
		},
		{
			name:   "tiny share clamps to one",
			shares: 1,
			want:   1,
		},
		{
			name:   "large share clamps to max",
			shares: 65535*uint64(ShareWeightRatio) + 64,
			want:   65535,
		},
	}

	for _, tt := range tests {
		if got := ShareToWeight(tt.shares); got != tt.want {
			t.Errorf("%s: ShareToWeight(%d) = %d, want %d", tt.name, tt.shares, got, tt.want)
		}
	}
}
