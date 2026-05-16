package task

const (
	signalInterrupt uint32 = 2
	signalKill      uint32 = 9
	signalTerminate uint32 = 15
	signalContinue  uint32 = 18
	signalStop      uint32 = 19
)

type killSignalAction int

const (
	killSignalIgnore killSignalAction = iota
	killSignalStopTask
	killSignalPauseTask
	killSignalResumeTask
)

type signalPolicy struct {
	signal uint32
	action killSignalAction
}

var killSignalPolicies = []signalPolicy{
	{signalInterrupt, killSignalStopTask},
	{signalKill, killSignalStopTask},
	{signalTerminate, killSignalStopTask},
	{signalStop, killSignalPauseTask},
	{signalContinue, killSignalResumeTask},
}

var killSignalPolicyBySignal = func() map[uint32]killSignalAction {
	rules := make(map[uint32]killSignalAction, len(killSignalPolicies))
	for _, policy := range killSignalPolicies {
		rules[policy.signal] = policy.action
	}
	return rules
}()

func classifyKillSignal(signal uint32) killSignalAction {
	if action, ok := killSignalPolicyBySignal[signal]; ok {
		return action
	}
	return killSignalIgnore
}
