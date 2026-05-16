package lifecycle

import (
	"micrun/internal/ports"
	"micrun/internal/support/channels"
	"micrun/internal/support/validation"
)

func signalTaskIOExit(taskHandle ports.Task) {
	if validation.IsNil(taskHandle) {
		return
	}
	taskHandle.IOExit()
	channels.Close(taskHandle.ExitChan())
}
