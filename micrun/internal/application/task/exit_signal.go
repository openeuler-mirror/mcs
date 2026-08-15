package task

import (
	"micrun/internal/ports"
)

// closeTaskExitSignal closes the task's exit channel via IOExit(), which uses
// sync.Once to ensure the close happens exactly once. Do NOT call
// channels.Close on ExitChan() directly — that would bypass the Once and
// cause a subsequent IOExit() to panic on close-of-closed-channel.
func closeTaskExitSignal(taskHandle ports.Task) {
	taskHandle.IOExit()
}
