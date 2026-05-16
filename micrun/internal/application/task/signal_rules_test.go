package task

import "testing"

func TestClassifyKillSignal(t *testing.T) {
	tests := map[uint32]killSignalAction{
		signalInterrupt: killSignalStopTask,
		signalKill:      killSignalStopTask,
		signalTerminate: killSignalStopTask,
		signalStop:      killSignalPauseTask,
		signalContinue:  killSignalResumeTask,
		0:               killSignalIgnore,
	}

	for signal, want := range tests {
		if got := classifyKillSignal(signal); got != want {
			t.Fatalf("classifyKillSignal(%d) = %v, want %v", signal, got, want)
		}
	}
}
