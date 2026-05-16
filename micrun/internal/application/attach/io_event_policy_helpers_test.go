package attach

import "micrun/internal/ports"

func makeIOEventPolicy(eventType ports.IOEventType, handler ioEventHandler) ioEventPolicy {
	policy, err := makeIOEventPolicyChecked(eventType, handler)
	if err != nil {
		panic(err)
	}
	return policy
}

func makeIOEventPolicyWithStop(eventType ports.IOEventType, reason ioStopReason, handler ioEventHandler) ioEventPolicy {
	policy, err := makeIOEventPolicyWithStopChecked(eventType, reason, handler)
	if err != nil {
		panic(err)
	}
	return policy
}

func validateIOEventPolicy(policy ioEventPolicy) ioEventPolicy {
	validated, err := validateIOEventPolicyChecked(policy)
	if err != nil {
		panic(err)
	}
	return validated
}

func mergeIOEventPolicies(base ioEventPolicySet, overrides []ioEventPolicy) ioEventPolicySet {
	merged, err := mergeIOEventPoliciesWithModeChecked(base, overrides, overrideRejectDuplicate)
	if err != nil {
		panic(err)
	}
	return merged
}

func mergeIOEventPoliciesFromOptions(base ioEventPolicySet, overrides []ioEventPolicy) ioEventPolicySet {
	merged, err := mergeIOEventPoliciesFromOptionsChecked(base, overrides)
	if err != nil {
		panic(err)
	}
	return merged
}

func mergeIOEventPoliciesWithMode(base ioEventPolicySet, overrides []ioEventPolicy, mode ioEventPolicyMergeMode) ioEventPolicySet {
	merged, err := mergeIOEventPoliciesWithModeChecked(base, overrides, mode)
	if err != nil {
		panic(err)
	}
	return merged
}

func clonePolicySetForMerge(base ioEventPolicySet, extraCapacity int) ioEventPolicySet {
	cloned, err := clonePolicySetForMergeChecked(base, extraCapacity)
	if err != nil {
		panic(err)
	}
	return cloned
}

func buildIOEventPoliciesByType(policies []ioEventPolicy) ioEventPolicySet {
	set, err := buildIOEventPoliciesByTypeChecked(policies)
	if err != nil {
		panic(err)
	}
	return set
}

func validateIOEventPolicySet(set ioEventPolicySet) ioEventPolicySet {
	validated, err := validateIOEventPolicySetChecked(set)
	if err != nil {
		panic(err)
	}
	return validated
}

func newIOEventProfile(set ioEventPolicySet) ioEventProfile {
	profile, err := newIOEventProfileChecked(set)
	if err != nil {
		panic(err)
	}
	return profile
}
