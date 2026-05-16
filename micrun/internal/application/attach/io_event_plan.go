package attach

type ioEventPlan struct {
	hasStopReason bool
	stopReason    ioStopReason
}

func stopEventPlan(reason ioStopReason) ioEventPlan {
	return ioEventPlan{
		hasStopReason: true,
		stopReason:    reason,
	}
}

func (p ioEventPlan) stopReasonValue() (ioStopReason, bool) {
	if !p.hasStopReason {
		return ioStopReason{}, false
	}
	return p.stopReason, true
}
