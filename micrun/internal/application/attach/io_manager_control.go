package attach

import (
	"micrun/internal/ports"
	"micrun/internal/support/validation"
)

func loadIOManager(taskHandle ports.Task) (ports.IOManager, bool) {
	if validation.IsNil(taskHandle) {
		return nil, false
	}
	manager := taskHandle.IOManager()
	if validation.IsNil(manager) {
		return nil, false
	}
	return manager, true
}

func withIOManager(taskHandle ports.Task, fn func(ports.IOManager)) {
	manager, ok := loadIOManager(taskHandle)
	if !ok {
		return
	}
	fn(manager)
}

func stopIOManager(taskHandle ports.Task) {
	withIOManager(taskHandle, func(manager ports.IOManager) {
		stopLoadedIOManager(manager)
	})
}

func stopAndClearIOManager(taskHandle ports.Task) {
	withIOManager(taskHandle, func(manager ports.IOManager) {
		stopLoadedIOManager(manager)
		taskHandle.SetIOManager(nil)
	})
}

func detachIOManager(taskHandle ports.Task) {
	withIOManager(taskHandle, func(manager ports.IOManager) {
		detachLoadedIOManager(manager)
	})
}

func stopLoadedIOManager(manager ports.IOManager) {
	if validation.IsNil(manager) {
		return
	}
	manager.Stop()
}

func detachLoadedIOManager(manager ports.IOManager) {
	if validation.IsNil(manager) {
		return
	}
	manager.StopWithoutClosingFIFOs()
}
