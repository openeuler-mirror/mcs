package attach

import (
	"fmt"

	"micrun/internal/ports"
)

type ioEventPolicy struct {
	eventType ports.IOEventType
	plan      ioEventPlan
	handler   ioEventHandler
	// requiresStopReason indicates this event policy intentionally carries stop semantics.
	// It is set by constructors that include a stop reason.
	requiresStopReason bool
}

func makeIOEventPolicyChecked(eventType ports.IOEventType, handler ioEventHandler) (ioEventPolicy, error) {
	return validateIOEventPolicyChecked(ioEventPolicy{eventType: eventType, handler: handler})
}

func makeIOEventPolicyWithStopChecked(eventType ports.IOEventType, reason ioStopReason, handler ioEventHandler) (ioEventPolicy, error) {
	return validateIOEventPolicyChecked(ioEventPolicy{
		eventType:          eventType,
		plan:               stopEventPlan(reason),
		handler:            handler,
		requiresStopReason: true,
	})
}

func validateIOEventPolicyChecked(policy ioEventPolicy) (ioEventPolicy, error) {
	if policy.handler == nil {
		return ioEventPolicy{}, fmt.Errorf("IO event policy for %v is missing handler", policy.eventType)
	}
	if policy.requiresStopReason && !policy.plan.hasStopReason {
		return ioEventPolicy{}, fmt.Errorf("IO event policy for %v is stop policy without stop reason", policy.eventType)
	}
	if !policy.requiresStopReason && policy.plan.hasStopReason {
		return ioEventPolicy{}, fmt.Errorf("IO event policy for %v has stop reason but is not a stop policy", policy.eventType)
	}
	return policy, nil
}
