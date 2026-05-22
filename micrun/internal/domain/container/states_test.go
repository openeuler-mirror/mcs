package container

import "testing"

func TestStateStringValidAcceptsKnownOperationalStates(t *testing.T) {
	for _, state := range []StateString{StateReady, StateRunning, StateStopped, StateCreating, StatePaused} {
		t.Run(string(state), func(t *testing.T) {
			if !state.valid() {
				t.Fatalf("%s should be valid", state)
			}
		})
	}
}

func TestStateStringValidRejectsDownAndUnknownStates(t *testing.T) {
	for _, state := range []StateString{StateDown, "", "unknown"} {
		t.Run(string(state), func(t *testing.T) {
			if state.valid() {
				t.Fatalf("%q should be invalid", state)
			}
		})
	}
}

func TestStateStringKnownIncludesDownButRejectsUnknown(t *testing.T) {
	for _, state := range []StateString{StateReady, StateRunning, StateStopped, StateCreating, StatePaused, StateDown} {
		t.Run(string(state), func(t *testing.T) {
			if !state.known() {
				t.Fatalf("%q should be known", state)
			}
		})
	}

	unknown := StateString("unknown")
	if unknown.known() {
		t.Fatal("unknown state should not be known")
	}
}

func TestSetSandboxStateRejectsNonOperationalStates(t *testing.T) {
	sandbox := &Sandbox{id: "sandbox-state-test"}
	for _, state := range []StateString{StateDown, "", "unknown"} {
		t.Run(string(state), func(t *testing.T) {
			if err := sandbox.SetSandboxState(state); err == nil {
				t.Fatalf("SetSandboxState(%q) expected error, got nil", state)
			}
		})
	}
}
