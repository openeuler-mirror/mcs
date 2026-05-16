package attach

import (
	"reflect"
	"testing"

	"micrun/internal/ports"
)

func TestSessionEventTypes(t *testing.T) {
	svc := NewService(nil)
	got := svc.sessionEventTypesSnapshot()
	want := defaultIOEventPolicySet.eventTypes()

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sessionEventTypes = %v, want %v", got, want)
	}
}

func TestSessionEventTypesReturnsCopy(t *testing.T) {
	svc := NewService(nil)
	types := svc.sessionEventTypesSnapshot()
	if len(types) == 0 {
		t.Fatal("expected non-empty session event types")
	}

	types[0] = ports.IOEventError

	refreshed := svc.sessionEventTypesSnapshot()
	if !reflect.DeepEqual(refreshed, defaultIOEventPolicySet.eventTypes()) {
		t.Fatal("sessionEventTypes should return immutable copy")
	}
}

func TestSessionEventTypesReflectsInjectedPolicies(t *testing.T) {
	svc := NewService(nil, WithIOEventPolicies([]ioEventPolicy{
		makeIOEventPolicy(ports.IOEventTTYReady, handleIOEventReportError),
		makeIOEventPolicy(ports.IOEventExitCommand, handleIOEventReportError),
	}))
	types := svc.sessionEventTypesSnapshot()

	want := []ports.IOEventType{
		ports.IOEventExitCommand,
		ports.IOEventInterrupt,
		ports.IOEventStdinClosed,
		ports.IOEventDetach,
		ports.IOEventError,
		ports.IOEventTTYReady,
	}
	if !reflect.DeepEqual(types, want) {
		t.Fatalf("sessionEventTypes = %v, want %v", types, want)
	}
}
