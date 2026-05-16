package defs

import "testing"

func TestIsSupportedGuestOS(t *testing.T) {
	tests := []struct {
		name string
		os   string
		want bool
	}{
		{name: "default os", os: DefaultOS, want: true},
		{name: "zephyr", os: "zephyr", want: true},
		{name: "linux", os: "linux", want: false},
		{name: "empty", os: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSupportedGuestOS(tt.os); got != tt.want {
				t.Fatalf("IsSupportedGuestOS(%q) = %v, want %v", tt.os, got, tt.want)
			}
		})
	}
}

func TestSupportedGuestOSReturnsSortedCopy(t *testing.T) {
	got := SupportedGuestOS()
	want := []string{"uniproton", "zephyr"}
	if len(got) != len(want) {
		t.Fatalf("SupportedGuestOS() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SupportedGuestOS()[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	got[0] = "linux"
	if IsSupportedGuestOS("linux") {
		t.Fatal("SupportedGuestOS() returned mutable backing storage")
	}
}
