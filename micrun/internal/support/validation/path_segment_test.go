package validation

import (
	"errors"
	"testing"
)

func TestIsSinglePathSegment(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{
			name:  "simple",
			value: "container-1",
			want:  true,
		},
		{
			name:  "empty",
			value: "",
			want:  false,
		},
		{
			name:  "leading whitespace",
			value: " container-1",
			want:  false,
		},
		{
			name:  "trailing whitespace",
			value: "container-1 ",
			want:  false,
		},
		{
			name:  "dot",
			value: ".",
			want:  false,
		},
		{
			name:  "dot dot",
			value: "..",
			want:  false,
		},
		{
			name:  "forward separator",
			value: "parent/container",
			want:  false,
		},
		{
			name:  "backslash separator",
			value: `parent\container`,
			want:  false,
		},
		{
			name:  "nul byte",
			value: "container\x00id",
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSinglePathSegment(tc.value); got != tc.want {
				t.Fatalf("IsSinglePathSegment(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestNormalizeSinglePathSegment(t *testing.T) {
	got, err := NormalizeSinglePathSegment("container-1")
	if err != nil {
		t.Fatalf("NormalizeSinglePathSegment returned error: %v", err)
	}
	if got != "container-1" {
		t.Fatalf("NormalizeSinglePathSegment = %q, want container-1", got)
	}

	for _, value := range []string{"", " container-1", "container-1 ", "parent/container", `parent\container`, ".", "..", "container\x00id"} {
		t.Run(value, func(t *testing.T) {
			if _, err := NormalizeSinglePathSegment(value); !errors.Is(err, ErrInvalidPathSegment) {
				t.Fatalf("NormalizeSinglePathSegment(%q) error = %v, want ErrInvalidPathSegment", value, err)
			}
		})
	}
}
