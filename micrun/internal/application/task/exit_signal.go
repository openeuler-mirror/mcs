package task

import (
	"micrun/internal/ports"
	"micrun/internal/support/channels"
)

func closeTaskExitSignal(taskHandle ports.Task) {
	channels.Close(taskHandle.ExitChan())
}
