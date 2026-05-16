package exitstatus

import "testing"

func TestFromSignalUsesShellExitConvention(t *testing.T) {
	if got := FromSignal(2); got != 130 {
		t.Fatalf("expected SIGINT exit status 130, got %d", got)
	}
	if got := FromSignal(15); got != 143 {
		t.Fatalf("expected SIGTERM exit status 143, got %d", got)
	}
}

func TestInterruptExitStatus(t *testing.T) {
	if got := Interrupt(); got != FromSignal(SignalInterrupt) {
		t.Fatalf("expected interrupt status %d, got %d", FromSignal(SignalInterrupt), got)
	}
}
