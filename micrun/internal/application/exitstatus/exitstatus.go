package exitstatus

const (
	Success uint32 = 0

	SignalInterrupt uint32 = 2

	signalExitOffset uint32 = 128
)

func FromSignal(signal uint32) uint32 {
	return signalExitOffset + signal
}

func Interrupt() uint32 {
	return FromSignal(SignalInterrupt)
}
