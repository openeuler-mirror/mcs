package attach

import (
	"reflect"
	"testing"

	"micrun/internal/application/exitstatus"
	"micrun/internal/ports"
)

func TestServiceResolveIOEventPolicy(t *testing.T) {
	svc := NewService(nil)
	tests := []struct {
		name      string
		task      string
		event     ports.IOEvent
		want      ioEventPlan
		wantMatch bool
	}{
		{
			name:      "ignores other task",
			task:      "task-a",
			wantMatch: false,
			event: ports.IOEvent{
				Type:        ports.IOEventExitCommand,
				ContainerID: "task-b",
			},
			want: ioEventPlan{},
		},
		{
			name:      "ignores empty container id",
			task:      "task-a",
			wantMatch: false,
			event: ports.IOEvent{
				Type: ports.IOEventExitCommand,
			},
			want: ioEventPlan{},
		},
		{
			name:      "exit command stops task",
			task:      "task-a",
			wantMatch: true,
			event: ports.IOEvent{
				Type:        ports.IOEventExitCommand,
				ContainerID: "task-a",
			},
			want: stopEventPlan(ioStopByExitCommand),
		},
		{
			name:      "interrupt stops task",
			task:      "task-a",
			wantMatch: true,
			event: ports.IOEvent{
				Type:        ports.IOEventInterrupt,
				ContainerID: "task-a",
			},
			want: stopEventPlan(ioStopByInterrupt),
		},
		{
			name:      "stdin close stops session",
			task:      "task-a",
			wantMatch: true,
			event: ports.IOEvent{
				Type:        ports.IOEventStdinClosed,
				ContainerID: "task-a",
			},
			want: ioEventPlan{},
		},
		{
			name:      "detach preserves session resources",
			task:      "task-a",
			wantMatch: true,
			event: ports.IOEvent{
				Type:        ports.IOEventDetach,
				ContainerID: "task-a",
			},
			want: ioEventPlan{},
		},
		{
			name:      "error is reported",
			task:      "task-a",
			wantMatch: true,
			event: ports.IOEvent{
				Type:        ports.IOEventError,
				ContainerID: "task-a",
			},
			want: ioEventPlan{},
		},
		{
			name:      "tty ready is ignored by attach service",
			task:      "task-a",
			wantMatch: false,
			event: ports.IOEvent{
				Type:        ports.IOEventTTYReady,
				ContainerID: "task-a",
			},
			want: ioEventPlan{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, matched := svc.resolveIOEventPolicy(tt.task, tt.event)
			if matched != tt.wantMatch {
				t.Fatalf("resolveIOEventPolicy() matched=%v, want=%v", matched, tt.wantMatch)
			}
			if matched && got.handler == nil {
				t.Fatalf("resolveIOEventPolicy() expected non-nil handler when matched=%v", matched)
			}
			if !matched && got.handler != nil {
				t.Fatalf("resolveIOEventPolicy() returned handler when matched=%v", matched)
			}
			if !isEquivalentIOEventPlan(got.plan, tt.want) {
				t.Fatalf("resolveIOEventPolicy() = %+v, want %+v", got.plan, tt.want)
			}
		})
	}
}

func TestIOEventProfileResolveTaskIOEvent(t *testing.T) {
	profile := newIOEventProfile(buildIOEventPoliciesByType([]ioEventPolicy{
		makeIOEventPolicyWithStop(ports.IOEventExitCommand, ioStopByExitCommand, handleIOEventStopTask),
		makeIOEventPolicy(ports.IOEventDetach, handleIOEventDetach),
	}))

	t.Run("resolves_matching_task", func(t *testing.T) {
		got, ok := profile.resolveTaskIOEvent("container-a", ports.IOEvent{
			Type:        ports.IOEventExitCommand,
			ContainerID: "container-a",
		})
		if !ok {
			t.Fatal("expected matching task event to resolve")
		}
		reason, hasReason := got.plan.stopReasonValue()
		if !hasReason {
			t.Fatal("expected resolved policy to carry stop reason")
		}
		if reason != ioStopByExitCommand {
			t.Fatalf("resolved stop reason = %+v, want %+v", reason, ioStopByExitCommand)
		}
	})

	t.Run("ignores_mismatched_task", func(t *testing.T) {
		_, ok := profile.resolveTaskIOEvent("container-a", ports.IOEvent{
			Type:        ports.IOEventExitCommand,
			ContainerID: "container-b",
		})
		if ok {
			t.Fatal("expected mismatched container to ignore event")
		}
	})

	t.Run("ignores_empty_container", func(t *testing.T) {
		_, ok := profile.resolveTaskIOEvent("container-a", ports.IOEvent{
			Type: ports.IOEventDetach,
		})
		if ok {
			t.Fatal("expected empty container id to ignore event")
		}
	})

	t.Run("missing_profile_is_empty", func(t *testing.T) {
		var empty ioEventProfile
		_, ok := empty.resolveTaskIOEvent("container-a", ports.IOEvent{
			Type:        ports.IOEventDetach,
			ContainerID: "container-a",
		})
		if ok {
			t.Fatal("expected empty profile to have no policy")
		}
	})

	t.Run("profile_owns_policy_snapshot", func(t *testing.T) {
		set := buildIOEventPoliciesByType([]ioEventPolicy{
			makeIOEventPolicy(ports.IOEventDetach, handleIOEventDetach),
		})
		profile := newIOEventProfile(set)

		set.applyIOEventPolicy(makeIOEventPolicy(ports.IOEventError, handleIOEventReportError))
		if _, ok := profile.policy(ports.IOEventError); ok {
			t.Fatal("expected profile to ignore later policy set mutations")
		}
	})

	t.Run("profile_validates_injected_policy_set", func(t *testing.T) {
		defer func() {
			if err := recover(); err == nil {
				t.Fatal("expected panic for invalid policy set")
			}
		}()

		_ = newIOEventProfile(ioEventPolicySet{
			byType: map[ports.IOEventType]ioEventPolicy{
				ports.IOEventDetach: makeIOEventPolicy(ports.IOEventDetach, handleIOEventDetach),
			},
		})
	})
}

func TestBuildIOEventPoliciesByType(t *testing.T) {
	t.Run("make_policy_rejects_missing_handler", func(t *testing.T) {
		t.Helper()

		defer func() {
			err := recover()
			if err == nil {
				t.Fatal("expected panic for missing IO event handler")
			}
		}()

		_ = makeIOEventPolicy(ports.IOEventExitCommand, nil)
	})

	t.Run("rejects_missing_handler", func(t *testing.T) {
		t.Helper()

		defer func() {
			err := recover()
			if err == nil {
				t.Fatal("expected panic for missing IO event handler")
			}
			if _, ok := err.(error); !ok {
				t.Fatalf("expected panic message, got %#v", err)
			}
		}()

		_ = buildIOEventPoliciesByType([]ioEventPolicy{
			{
				eventType: ports.IOEventExitCommand,
				plan:      stopEventPlan(ioStopByExitCommand),
			},
		})
	})

	t.Run("rejects_stop_handler_without_stop_reason", func(t *testing.T) {
		t.Helper()

		defer func() {
			err := recover()
			if err == nil {
				t.Fatal("expected panic for stop handler without stop reason")
			}
		}()

		_ = buildIOEventPoliciesByType([]ioEventPolicy{
			{
				eventType:          ports.IOEventExitCommand,
				requiresStopReason: true,
				handler:            handleIOEventStopTask,
			},
		})
	})

	t.Run("rejects_stop_reason_without_stop_policy", func(t *testing.T) {
		t.Helper()

		defer func() {
			err := recover()
			if err == nil {
				t.Fatal("expected panic for stop reason without stop policy marker")
			}
		}()

		_ = buildIOEventPoliciesByType([]ioEventPolicy{
			{
				eventType:          ports.IOEventExitCommand,
				plan:               stopEventPlan(ioStopByExitCommand),
				requiresStopReason: false,
				handler:            handleIOEventDetach,
			},
		})
	})

	t.Run("stop_constructor_marks_policy_as_requires_stop_reason", func(t *testing.T) {
		t.Helper()

		policy := makeIOEventPolicyWithStop(ports.IOEventExitCommand, ioStopByExitCommand, handleIOEventStopTask)
		if policy.requiresStopReason == false {
			t.Fatal("expected policy marked as requires stop reason after makeIOEventPolicyWithStop")
		}
		if _, ok := policy.plan.stopReasonValue(); !ok {
			t.Fatal("expected stop reason from makeIOEventPolicyWithStop")
		}
	})

	t.Run("rejects_duplicated_event_type_in_base", func(t *testing.T) {
		t.Helper()

		defer func() {
			err := recover()
			if err == nil {
				t.Fatal("expected panic for duplicated IO event policy type")
			}
		}()

		_ = buildIOEventPoliciesByType([]ioEventPolicy{
			makeIOEventPolicy(ports.IOEventDetach, handleIOEventDetach),
			makeIOEventPolicyWithStop(ports.IOEventExitCommand, ioStopByExitCommand, handleIOEventStopTask),
			makeIOEventPolicy(ports.IOEventDetach, handleIOEventDetach),
		})
	})

	t.Run("rejects_duplicate_event_type", func(t *testing.T) {
		t.Helper()

		defer func() {
			err := recover()
			if err == nil {
				t.Fatal("expected panic for duplicated IO event policy")
			}
			if _, ok := err.(error); !ok {
				t.Fatalf("expected panic message, got %#v", err)
			}
		}()

		_ = buildIOEventPoliciesByType([]ioEventPolicy{
			{
				eventType: ports.IOEventError,
				plan:      ioEventPlan{},
				handler:   handleIOEventReportError,
			},
			{
				eventType: ports.IOEventError,
				plan:      ioEventPlan{},
				handler:   handleIOEventReportError,
			},
		})
	})
}

func TestMergeIOEventPoliciesFromOptionsKeepsLastForDuplicateEventType(t *testing.T) {
	first := makeIOEventPolicy(ports.IOEventError, handleIOEventReportError)
	second := makeIOEventPolicy(ports.IOEventStdinClosed, handleIOEventStdinClosed)
	third := makeIOEventPolicyWithStop(ports.IOEventError, ioStopReason{name: "dedupe", exitStatus: 77}, handleIOEventStopTask)

	merged := mergeIOEventPoliciesFromOptions(ioEventPolicySet{}, []ioEventPolicy{
		first,
		second,
		third,
	})
	eventTypes := merged.eventTypes()

	if len(eventTypes) != 2 {
		t.Fatalf("merged policies = %d, want 2", len(eventTypes))
	}
	if eventTypes[0] != ports.IOEventError {
		t.Fatalf("first event type = %v, want %v", eventTypes[0], ports.IOEventError)
	}
	if eventTypes[1] != ports.IOEventStdinClosed {
		t.Fatalf("second event type = %v, want %v", eventTypes[1], ports.IOEventStdinClosed)
	}
	reason, ok := merged.stopReasonForEvent(ports.IOEventError)
	if !ok || reason.exitStatus != 77 {
		t.Fatalf("last duplicate policy for error should win, got reason=%+v ok=%v", reason, ok)
	}
}

func TestMergeIOEventPoliciesFromOptionsNoDuplicate(t *testing.T) {
	policies := []ioEventPolicy{
		makeIOEventPolicyWithStop(ports.IOEventExitCommand, ioStopByExitCommand, handleIOEventStopTask),
		makeIOEventPolicy(ports.IOEventError, handleIOEventReportError),
	}

	merged := mergeIOEventPoliciesFromOptions(ioEventPolicySet{}, policies)
	eventTypes := merged.eventTypes()
	if len(eventTypes) != len(policies) {
		t.Fatalf("merged length changed unexpectedly, got %d want %d", len(eventTypes), len(policies))
	}
	if eventTypes[0] != policies[0].eventType {
		t.Fatalf("first merged policy mismatch")
	}
	if eventTypes[1] != policies[1].eventType {
		t.Fatalf("second deduped policy mismatch")
	}
}

func TestMergeIOEventPoliciesFromOptionsTwoDuplicates(t *testing.T) {
	first := makeIOEventPolicyWithStop(ports.IOEventError, ioStopReason{name: "first", exitStatus: 1}, handleIOEventStopTask)
	second := makeIOEventPolicyWithStop(ports.IOEventError, ioStopReason{name: "second", exitStatus: 2}, handleIOEventStopTask)

	merged := mergeIOEventPoliciesFromOptions(ioEventPolicySet{}, []ioEventPolicy{first, second})
	eventTypes := merged.eventTypes()
	if len(eventTypes) != 1 {
		t.Fatalf("merged length = %d, want 1", len(eventTypes))
	}
	reason, ok := merged.stopReasonForEvent(ports.IOEventError)
	if !ok {
		t.Fatal("expected stop reason from last duplicate")
	}
	if reason.exitStatus != 2 || reason.name != "second" {
		t.Fatalf("expected second duplicate to win, got %+v", reason)
	}
}

func TestApplyIOEventPolicyInitializesMissingStorage(t *testing.T) {
	var set ioEventPolicySet
	policy := makeIOEventPolicy(ports.IOEventError, handleIOEventReportError)

	set.applyIOEventPolicy(policy)

	stored, ok := set.policy(ports.IOEventError)
	if !ok {
		t.Fatal("expected policy stored on zero-value set")
	}
	if len(set.orderedEventTypes) != 1 {
		t.Fatalf("ordered event types count = %d, want 1", len(set.orderedEventTypes))
	}
	if set.orderedEventTypes[0] != ports.IOEventError {
		t.Fatalf("ordered event type = %v, want %v", set.orderedEventTypes[0], ports.IOEventError)
	}
	if stored.eventType != ports.IOEventError {
		t.Fatalf("stored event type = %v, want %v", stored.eventType, ports.IOEventError)
	}
}

func TestDefaultIOEventPoliciesAreCheckedAndValidated(t *testing.T) {
	if len(defaultIOEventPolicySet.eventTypes()) == 0 {
		t.Fatal("default IO event policies should not be empty")
	}
	defaultPolicies := []ioEventPolicy{
		mustMakeIOEventPolicyWithStop(ports.IOEventExitCommand, ioStopByExitCommand, handleIOEventStopTask),
		mustMakeIOEventPolicyWithStop(ports.IOEventInterrupt, ioStopByInterrupt, handleIOEventStopTask),
		mustMakeIOEventPolicy(ports.IOEventStdinClosed, handleIOEventStdinClosed),
		mustMakeIOEventPolicy(ports.IOEventDetach, handleIOEventDetach),
		mustMakeIOEventPolicy(ports.IOEventError, handleIOEventReportError),
	}
	validated, err := buildIOEventPoliciesByTypeChecked(defaultPolicies)
	if err != nil {
		t.Fatalf("buildIOEventPoliciesByTypeChecked returned error: %v", err)
	}
	if !reflect.DeepEqual(validated.eventTypes(), defaultIOEventPolicySet.eventTypes()) {
		t.Fatalf("default event types = %v, rebuilt = %v", defaultIOEventPolicySet.eventTypes(), validated.eventTypes())
	}
}

func TestMergeIOEventPolicies(t *testing.T) {
	overrides := []ioEventPolicy{
		makeIOEventPolicyWithStop(ports.IOEventDetach, ioStopReason{name: "policy-detach", exitStatus: 42}, handleIOEventDetach),
	}
	merged := mergeIOEventPolicies(defaultIOEventPolicySet, overrides)

	if reason, ok := merged.stopReasonForEvent(ports.IOEventExitCommand); !ok || reason.exitStatus != exitstatus.Success {
		t.Fatalf("exit command reason = %+v, ok=%v", reason, ok)
	}
	if reason, ok := merged.stopReasonForEvent(ports.IOEventInterrupt); !ok || reason.exitStatus != exitstatus.Interrupt() {
		t.Fatalf("interrupt reason = %+v, ok=%v", reason, ok)
	}
	if reason, ok := merged.stopReasonForEvent(ports.IOEventDetach); !ok || reason.exitStatus != 42 {
		t.Fatalf("detach reason = %+v, ok=%v", reason, ok)
	}

	eventTypes := merged.eventTypes()
	if len(eventTypes) != len(defaultIOEventPolicySet.eventTypes()) {
		t.Fatalf("unexpected event type count %d, want %d", len(eventTypes), len(defaultIOEventPolicySet.eventTypes()))
	}
	if !reflect.DeepEqual(eventTypes, defaultIOEventPolicySet.eventTypes()) {
		t.Fatalf("unexpected event type order, got=%v want=%v", eventTypes, defaultIOEventPolicySet.eventTypes())
	}
}

func TestMergeIOEventPoliciesRejectsDuplicateOverride(t *testing.T) {
	t.Helper()

	defer func() {
		err := recover()
		if err == nil {
			t.Fatal("expected panic for duplicated override IO event policy")
		}
		if _, ok := err.(error); !ok {
			t.Fatalf("expected panic message, got %#v", err)
		}
	}()

	_ = mergeIOEventPolicies(defaultIOEventPolicySet, []ioEventPolicy{
		{
			eventType: ports.IOEventError,
			plan:      ioEventPlan{},
			handler:   handleIOEventReportError,
		},
		{
			eventType: ports.IOEventError,
			plan:      ioEventPlan{},
			handler:   handleIOEventReportError,
		},
	})
}

func TestMergeIOEventPoliciesFromOptionsLastWins(t *testing.T) {
	policyA := makeIOEventPolicyWithStop(ports.IOEventError, ioStopReason{name: "first", exitStatus: 1}, handleIOEventStopTask)
	policyB := makeIOEventPolicyWithStop(ports.IOEventError, ioStopReason{name: "second", exitStatus: 2}, handleIOEventStopTask)

	merged := mergeIOEventPoliciesFromOptions(defaultIOEventPolicySet, []ioEventPolicy{
		policyA,
		makeIOEventPolicy(ports.IOEventDetach, handleIOEventDetach),
		policyB,
	})

	if reason, ok := merged.stopReasonForEvent(ports.IOEventError); !ok || reason.exitStatus != 2 || reason.name != "second" {
		t.Fatalf("expected error reason to use last option, got %+v ok=%v", reason, ok)
	}
}

func TestMergeIOEventPoliciesWithoutOverridesReturnsIndependentCopy(t *testing.T) {
	base := makeIOEventPolicySetForTest([]ioEventPolicy{
		makeIOEventPolicyWithStop(ports.IOEventExitCommand, ioStopByExitCommand, handleIOEventStopTask),
		makeIOEventPolicy(ports.IOEventError, handleIOEventReportError),
	})
	baseOrdered := base.eventTypes()

	merged := mergeIOEventPolicies(base, nil)
	if len(merged.eventTypes()) != len(baseOrdered) {
		t.Fatalf("unexpected merged event count %d, want %d", len(merged.eventTypes()), len(baseOrdered))
	}

	merged.byType[ports.IOEventExitCommand] = makeIOEventPolicyWithStop(
		ports.IOEventExitCommand,
		ioStopReason{name: "mutated", exitStatus: 99},
		handleIOEventStopTask,
	)

	reason, ok := base.byType[ports.IOEventExitCommand].plan.stopReasonValue()
	if !ok {
		t.Fatal("expected base policy to keep stop reason")
	}
	if reason.exitStatus == 99 {
		t.Fatal("expected merge without overrides to not share map storage with base")
	}
}

func TestMergeIOEventPoliciesFromOptionsWithoutOverridesReturnsIndependentCopy(t *testing.T) {
	base := makeIOEventPolicySetForTest([]ioEventPolicy{
		makeIOEventPolicyWithStop(ports.IOEventExitCommand, ioStopByExitCommand, handleIOEventStopTask),
		makeIOEventPolicy(ports.IOEventError, handleIOEventReportError),
	})
	baseOrdered := base.eventTypes()

	merged := mergeIOEventPoliciesFromOptions(base, nil)
	if len(merged.eventTypes()) != len(baseOrdered) {
		t.Fatalf("unexpected merged event count %d, want %d", len(merged.eventTypes()), len(baseOrdered))
	}

	merged.byType[ports.IOEventExitCommand] = makeIOEventPolicyWithStop(
		ports.IOEventExitCommand,
		ioStopReason{name: "mutated", exitStatus: 99},
		handleIOEventStopTask,
	)

	reason, ok := base.byType[ports.IOEventExitCommand].plan.stopReasonValue()
	if !ok {
		t.Fatal("expected base policy to keep stop reason")
	}
	if reason.exitStatus == 99 {
		t.Fatal("expected merge with empty options to not share map storage with base")
	}
}

func makeIOEventPolicySetForTest(policies []ioEventPolicy) ioEventPolicySet {
	return buildIOEventPoliciesByType(policies)
}

func TestMergeIOEventPoliciesSupportsExtendingNewEventType(t *testing.T) {
	const customEventType ports.IOEventType = 99
	merged := mergeIOEventPolicies(defaultIOEventPolicySet, []ioEventPolicy{
		makeIOEventPolicy(customEventType, handleIOEventReportError),
		makeIOEventPolicyWithStop(ports.IOEventDetach, ioStopReason{name: "policy-detach", exitStatus: 42}, handleIOEventDetach),
	})

	eventTypes := merged.eventTypes()
	if len(eventTypes) != len(defaultIOEventPolicySet.eventTypes())+1 {
		t.Fatalf("unexpected event type count %d, want %d", len(eventTypes), len(defaultIOEventPolicySet.eventTypes())+1)
	}
	wantTypes := append(defaultIOEventPolicySet.eventTypes(), customEventType)
	if !reflect.DeepEqual(eventTypes, wantTypes) {
		t.Fatalf("unexpected event type order: got=%v want=%v", eventTypes, wantTypes)
	}

	reason, ok := merged.stopReasonForEvent(ports.IOEventDetach)
	if !ok || reason.exitStatus != 42 {
		t.Fatalf("expected override for detach to be applied, reason=%+v ok=%v", reason, ok)
	}
	if _, ok := merged.stopReasonForEvent(customEventType); ok {
		t.Fatalf("custom event type without stop plan should not be considered stop reason")
	}
}

func TestMergeIOEventPoliciesRejectsDuplicatedBaseEventType(t *testing.T) {
	t.Helper()

	policy := makeIOEventPolicyWithStop(ports.IOEventExitCommand, ioStopByExitCommand, handleIOEventStopTask)
	base := ioEventPolicySet{
		byType: map[ports.IOEventType]ioEventPolicy{
			ports.IOEventExitCommand: policy,
		},
		orderedEventTypes: []ports.IOEventType{
			ports.IOEventExitCommand,
			ports.IOEventExitCommand,
		},
	}

	defer func() {
		err := recover()
		if err == nil {
			t.Fatal("expected panic for duplicated base IO event policy type")
		}
	}()

	_ = mergeIOEventPolicies(base, []ioEventPolicy{
		makeIOEventPolicyWithStop(ports.IOEventInterrupt, ioStopByInterrupt, handleIOEventStopTask),
	})
}

func TestMergeIOEventPoliciesRejectsInconsistentBaseSet(t *testing.T) {
	basePolicy := makeIOEventPolicy(ports.IOEventExitCommand, handleIOEventStopTask)
	invalidBase := ioEventPolicySet{
		byType: map[ports.IOEventType]ioEventPolicy{
			ports.IOEventExitCommand: basePolicy,
			ports.IOEventStdinClosed: makeIOEventPolicy(ports.IOEventStdinClosed, handleIOEventStdinClosed),
		},
		orderedEventTypes: []ports.IOEventType{ports.IOEventExitCommand},
	}

	defer func() {
		err := recover()
		if err == nil {
			t.Fatal("expected panic for inconsistent base IO event policy set")
		}
	}()

	_ = mergeIOEventPolicies(invalidBase, nil)
}

func TestMergeIOEventPoliciesRejectsInconsistentBaseSetWithNilHandler(t *testing.T) {
	base := ioEventPolicySet{
		byType: map[ports.IOEventType]ioEventPolicy{
			ports.IOEventExitCommand: {
				eventType: ports.IOEventExitCommand,
				plan:      ioEventPlan{},
				handler:   nil,
			},
		},
		orderedEventTypes: []ports.IOEventType{ports.IOEventExitCommand},
	}

	defer func() {
		err := recover()
		if err == nil {
			t.Fatal("expected panic for base IO event policy set with nil handler")
		}
	}()

	_ = mergeIOEventPolicies(base, []ioEventPolicy{
		makeIOEventPolicy(ports.IOEventInterrupt, handleIOEventStopTask),
	})
}

func TestValidateIOEventPolicySetRejectsDuplicateEventTypeInList(t *testing.T) {
	base := ioEventPolicySet{
		byType: map[ports.IOEventType]ioEventPolicy{
			ports.IOEventExitCommand: makeIOEventPolicy(ports.IOEventExitCommand, handleIOEventStopTask),
			ports.IOEventStdinClosed: makeIOEventPolicy(ports.IOEventStdinClosed, handleIOEventStdinClosed),
		},
		orderedEventTypes: []ports.IOEventType{
			ports.IOEventExitCommand,
			ports.IOEventExitCommand,
		},
	}

	defer func() {
		err := recover()
		if err == nil {
			t.Fatal("expected panic for duplicated event type in base policy list")
		}
	}()

	_ = validateIOEventPolicySet(base)
}

func TestLookupIOEventPolicyForType(t *testing.T) {
	t.Run("matches_event_type", func(t *testing.T) {
		_, matched := defaultIOEventPolicySet.policy(ports.IOEventExitCommand)
		if !matched {
			t.Fatal("expected matched exit-command policy")
		}
	})

	t.Run("ignores_unknown_event", func(t *testing.T) {
		_, matched := defaultIOEventPolicySet.policy(ports.IOEventTTYReady)
		if matched {
			t.Fatal("did not expect policy match for TTY ready event")
		}
	})
}

func TestEventPolicySetEventTypesReturnsCopy(t *testing.T) {
	types := defaultIOEventPolicySet.eventTypes()
	if len(types) == 0 {
		t.Fatal("expected default policy set to expose event types")
	}

	types[0] = ports.IOEventError

	refreshed := defaultIOEventPolicySet.eventTypes()
	if refreshed[0] == ports.IOEventError {
		t.Fatal("eventTypes() should return a defensive copy")
	}
}

func TestMergeIOEventPoliciesDoesNotMutateBaseSet(t *testing.T) {
	base := defaultIOEventPolicySet
	baseTypes := base.eventTypes()

	merged := mergeIOEventPolicies(base, []ioEventPolicy{
		makeIOEventPolicyWithStop(ports.IOEventDetach, ioStopReason{name: "policy-detach", exitStatus: 42}, handleIOEventDetach),
	})

	if len(base.eventTypes()) != len(baseTypes) {
		t.Fatal("base event type count changed after merge")
	}
	for i, eventType := range base.eventTypes() {
		if eventType != baseTypes[i] {
			t.Fatalf("base event order changed at index %d: got %v want %v", i, eventType, baseTypes[i])
		}
	}

	detachReason, ok := merged.stopReasonForEvent(ports.IOEventDetach)
	if !ok || detachReason.exitStatus != 42 {
		t.Fatalf("expected merged detach stop reason override, got reason=%+v ok=%v", detachReason, ok)
	}
}

func isEquivalentIOEventPlan(left, right ioEventPlan) bool {
	leftReason, leftStop := left.stopReasonValue()
	rightReason, rightStop := right.stopReasonValue()
	return leftStop == rightStop && (!leftStop || leftReason == rightReason)
}
