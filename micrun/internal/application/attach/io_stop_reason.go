package attach

import (
	"micrun/internal/application/exitstatus"
	"micrun/internal/ports"
)

type ioStopReason struct {
	name       string
	exitStatus uint32
}

var (
	ioStopByExitCommand = ioStopReason{name: "exit command", exitStatus: exitstatus.Success}
	ioStopByInterrupt   = ioStopReason{name: "interrupt", exitStatus: exitstatus.Interrupt()}
)

func (s *Service) ioStopReasonForEvent(eventType ports.IOEventType) (ioStopReason, bool) {
	if s == nil {
		return ioStopReason{}, false
	}
	return s.eventProfile.stopReasonForEvent(eventType)
}
