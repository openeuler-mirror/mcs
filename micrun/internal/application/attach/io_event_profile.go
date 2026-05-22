package attach

import (
	"micrun/internal/ports"
)

type ioEventProfile struct {
	policies ioEventPolicySet
}

func newIOEventProfileChecked(set ioEventPolicySet) (ioEventProfile, error) {
	policies, err := clonePolicySetForMergeChecked(set, 0)
	if err != nil {
		return ioEventProfile{}, err
	}
	return ioEventProfile{
		policies: policies,
	}, nil
}

func (p ioEventProfile) policy(eventType ports.IOEventType) (ioEventPolicy, bool) {
	return p.policies.policy(eventType)
}

func (p ioEventProfile) resolveTaskIOEvent(taskID string, event ports.IOEvent) (ioEventPolicy, bool) {
	if event.ContainerID == "" || event.ContainerID != taskID {
		return ioEventPolicy{}, false
	}
	return p.policy(event.Type)
}

func (p ioEventProfile) stopReasonForEvent(eventType ports.IOEventType) (ioStopReason, bool) {
	return p.policies.stopReasonForEvent(eventType)
}

func (p ioEventProfile) eventTypesSnapshot() []ports.IOEventType {
	return p.policies.eventTypes()
}
