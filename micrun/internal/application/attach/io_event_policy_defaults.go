package attach

import "micrun/internal/ports"

var defaultIOEventPolicySet = mustBuildDefaultIOEventPolicySet()

func mustBuildDefaultIOEventPolicySet() ioEventPolicySet {
	policySet, err := buildIOEventPoliciesByTypeChecked([]ioEventPolicy{
		mustMakeIOEventPolicyWithStop(ports.IOEventExitCommand, ioStopByExitCommand, handleIOEventStopTask),
		mustMakeIOEventPolicyWithStop(ports.IOEventInterrupt, ioStopByInterrupt, handleIOEventStopTask),
		mustMakeIOEventPolicy(ports.IOEventStdinClosed, handleIOEventStdinClosed),
		mustMakeIOEventPolicy(ports.IOEventDetach, handleIOEventDetach),
		mustMakeIOEventPolicy(ports.IOEventError, handleIOEventReportError),
	})
	if err != nil {
		panic(err)
	}
	return policySet
}

func mustMakeIOEventPolicy(eventType ports.IOEventType, handler ioEventHandler) ioEventPolicy {
	policy, err := makeIOEventPolicyChecked(eventType, handler)
	if err != nil {
		panic(err)
	}
	return policy
}

func mustMakeIOEventPolicyWithStop(eventType ports.IOEventType, reason ioStopReason, handler ioEventHandler) ioEventPolicy {
	policy, err := makeIOEventPolicyWithStopChecked(eventType, reason, handler)
	if err != nil {
		panic(err)
	}
	return policy
}
