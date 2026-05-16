package validation

import (
	"strings"
	"testing"
)

func TestMissingRequirementsHandlesTypedNilValues(t *testing.T) {
	var fn func()
	var ptr *int

	missing := MissingRequirements(
		Required("function", fn),
		Required("pointer", ptr),
		Required("value", 1),
	)

	if len(missing) != 2 || missing[0] != "function" || missing[1] != "pointer" {
		t.Fatalf("MissingRequirements = %v, want function and pointer", missing)
	}
}

func TestRequireAllReportsMissingNames(t *testing.T) {
	err := RequireAll("missing dependencies", Required("alpha", nil), Required("beta", "ok"))
	if err == nil {
		t.Fatal("RequireAll returned nil, want error")
	}
	if got := err.Error(); !strings.Contains(got, "missing dependencies") || !strings.Contains(got, "alpha") {
		t.Fatalf("RequireAll error = %q, want message and missing name", got)
	}
}

func TestRequireAllPassesWhenComplete(t *testing.T) {
	if err := RequireAll("missing dependencies", Required("alpha", "ok")); err != nil {
		t.Fatalf("RequireAll returned error: %v", err)
	}
}
