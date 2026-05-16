package attach

import (
	"fmt"

	"micrun/internal/ports"
)

type ioEventPolicySet struct {
	byType            map[ports.IOEventType]ioEventPolicy
	orderedEventTypes []ports.IOEventType
	validated         bool
}

type ioEventPolicyMergeMode int

const (
	// overrideRejectDuplicate rejects duplicated override event types.
	overrideRejectDuplicate ioEventPolicyMergeMode = iota
	// overrideReplaceLast keeps the last occurrence for duplicated event types.
	overrideReplaceLast
)

func mergeIOEventPoliciesFromOptionsChecked(base ioEventPolicySet, overrides []ioEventPolicy) (ioEventPolicySet, error) {
	if len(overrides) == 0 {
		return clonePolicySetForMergeChecked(base, 0)
	}

	// Options are user input; duplicated event types use the last occurrence
	// from the same option call.
	return mergeIOEventPoliciesWithModeChecked(base, overrides, overrideReplaceLast)
}

func mergeIOEventPoliciesWithModeChecked(base ioEventPolicySet, overrides []ioEventPolicy, mode ioEventPolicyMergeMode) (ioEventPolicySet, error) {
	merged, err := clonePolicySetForMergeChecked(base, len(overrides))
	if err != nil {
		return ioEventPolicySet{}, err
	}
	if len(overrides) == 0 {
		return merged, nil
	}

	var seenOverride map[ports.IOEventType]struct{}
	if mode == overrideRejectDuplicate {
		seenOverride = make(map[ports.IOEventType]struct{}, len(overrides))
	}

	for _, policy := range overrides {
		policy, err = validateIOEventPolicyChecked(policy)
		if err != nil {
			return ioEventPolicySet{}, err
		}
		if seenOverride != nil {
			if _, exists := seenOverride[policy.eventType]; exists {
				return ioEventPolicySet{}, fmt.Errorf("IO event policy for %v already defined", policy.eventType)
			}
			seenOverride[policy.eventType] = struct{}{}
		}
		merged.applyIOEventPolicy(policy)
	}

	merged.validated = true
	return merged, nil
}

func clonePolicySetForMergeChecked(base ioEventPolicySet, extraCapacity int) (ioEventPolicySet, error) {
	if !base.validated && (len(base.orderedEventTypes) > 0 || len(base.byType) > 0) {
		var err error
		base, err = validateIOEventPolicySetChecked(base)
		if err != nil {
			return ioEventPolicySet{}, err
		}
	}

	if extraCapacity == 0 {
		return cloneIOEventPolicySet(base), nil
	}

	return cloneIOEventPolicySetWithCapacity(base, extraCapacity), nil
}

func cloneIOEventPolicySet(base ioEventPolicySet) ioEventPolicySet {
	return cloneIOEventPolicySetWithCapacity(base, 0)
}

func cloneIOEventPolicySetWithCapacity(base ioEventPolicySet, extraCapacity int) ioEventPolicySet {
	if len(base.byType) == 0 && len(base.orderedEventTypes) == 0 && extraCapacity == 0 {
		return ioEventPolicySet{}
	}
	cloned := ioEventPolicySet{
		byType:            make(map[ports.IOEventType]ioEventPolicy, len(base.byType)+extraCapacity),
		orderedEventTypes: make([]ports.IOEventType, len(base.orderedEventTypes), len(base.orderedEventTypes)+extraCapacity),
		validated:         base.validated,
	}
	copy(cloned.orderedEventTypes, base.orderedEventTypes)
	for eventType, policy := range base.byType {
		cloned.byType[eventType] = policy
	}
	return cloned
}

func (set *ioEventPolicySet) applyIOEventPolicy(policy ioEventPolicy) {
	if set.byType == nil {
		set.byType = make(map[ports.IOEventType]ioEventPolicy)
	}
	if _, exists := set.byType[policy.eventType]; !exists {
		set.orderedEventTypes = append(set.orderedEventTypes, policy.eventType)
	}
	set.byType[policy.eventType] = policy
	set.validated = false
}

func buildIOEventPoliciesByTypeChecked(policies []ioEventPolicy) (ioEventPolicySet, error) {
	if len(policies) == 0 {
		return ioEventPolicySet{}, nil
	}
	return mergeIOEventPoliciesWithModeChecked(ioEventPolicySet{}, policies, overrideRejectDuplicate)
}

func validateIOEventPolicySetChecked(set ioEventPolicySet) (ioEventPolicySet, error) {
	typesByList := make(map[ports.IOEventType]struct{}, len(set.orderedEventTypes))

	for _, eventType := range set.orderedEventTypes {
		if _, exists := typesByList[eventType]; exists {
			return ioEventPolicySet{}, fmt.Errorf("IO event policy set is inconsistent: duplicated event type in list: %v", eventType)
		}
		typesByList[eventType] = struct{}{}
		if _, ok := set.byType[eventType]; !ok {
			return ioEventPolicySet{}, fmt.Errorf("IO event policy set is inconsistent: event type %v is in list but missing mapping", eventType)
		}
		if set.byType[eventType].handler == nil {
			return ioEventPolicySet{}, fmt.Errorf("IO event policy set is inconsistent: event type %v has nil handler", eventType)
		}
	}

	if len(set.byType) != len(set.orderedEventTypes) {
		return ioEventPolicySet{}, fmt.Errorf(
			"IO event policy set is inconsistent: event type count mismatch, byType=%d list=%d",
			len(set.byType),
			len(set.orderedEventTypes),
		)
	}

	for eventType := range set.byType {
		if _, ok := typesByList[eventType]; !ok {
			return ioEventPolicySet{}, fmt.Errorf("IO event policy set is inconsistent: event type %v in map is missing from list", eventType)
		}
	}

	set.validated = true
	return set, nil
}

func (set ioEventPolicySet) policy(eventType ports.IOEventType) (ioEventPolicy, bool) {
	if set.byType == nil {
		return ioEventPolicy{}, false
	}
	policy, ok := set.byType[eventType]
	return policy, ok
}

func (set ioEventPolicySet) stopReasonForEvent(eventType ports.IOEventType) (ioStopReason, bool) {
	policy, ok := set.policy(eventType)
	if !ok {
		return ioStopReason{}, false
	}
	stopReason, ok := policy.plan.stopReasonValue()
	if !ok {
		return ioStopReason{}, false
	}
	return stopReason, true
}

func cloneIOEventTypes(eventTypes []ports.IOEventType) []ports.IOEventType {
	cloned := make([]ports.IOEventType, len(eventTypes))
	copy(cloned, eventTypes)
	return cloned
}

func (set ioEventPolicySet) eventTypes() []ports.IOEventType {
	return cloneIOEventTypes(set.orderedEventTypes)
}
