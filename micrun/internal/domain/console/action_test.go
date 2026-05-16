package console

import "testing"

func TestEmitEventCarriesStopMode(t *testing.T) {
	tests := []struct {
		name  string
		event EventKind
		want  ActionStopMode
	}{
		{name: "exit closes", event: EventExitCommand, want: ActionStopClose},
		{name: "interrupt closes", event: EventInterrupt, want: ActionStopClose},
		{name: "detach preserves", event: EventDetach, want: ActionStopPreserve},
		{name: "none does not stop", event: EventNone, want: ActionStopNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := emitEvent(tt.event)
			if got := action.StopMode; got != tt.want {
				t.Fatalf("StopMode = %v, want %v", got, tt.want)
			}
			if got := action.EffectiveStopMode(); got != tt.want {
				t.Fatalf("EffectiveStopMode = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEffectiveStopModeDerivesEventStopMode(t *testing.T) {
	action := Action{Kind: ActionEmitEvent, Event: EventDetach}

	if got := action.EffectiveStopMode(); got != ActionStopPreserve {
		t.Fatalf("zero action stop mode = %v, want preserve for detach event", got)
	}
}

func TestEffectiveStopModeAllowsExplicitOverride(t *testing.T) {
	action := Action{Kind: ActionEmitEvent, Event: EventDetach, StopMode: ActionStopClose}

	if got := action.EffectiveStopMode(); got != ActionStopClose {
		t.Fatalf("explicit action stop mode = %v, want close", got)
	}
}
