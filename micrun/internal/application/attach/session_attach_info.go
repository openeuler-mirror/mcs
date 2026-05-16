package attach

import "micrun/internal/ports"

type attachSessionInfoRequest struct {
	factory    ports.IOSessionFactory
	namespace  string
	taskID     string
	terminal   bool
	attachInfo *ports.AttachInfo
	freshTTY   freshTTYHandles
}

func buildAttachSessionInfo(request attachSessionInfoRequest) ports.AttachInfo {
	attachInfo := cloneAttachInfo(request.attachInfo)
	if request.factory != nil {
		paths := resolveAttachFIFOPaths(request.factory, request.namespace, request.taskID, request.terminal, &attachInfo)
		paths.applyTo(&attachInfo)
	}
	if request.freshTTY.present() {
		attachInfo = request.freshTTY.attachInfo(&attachInfo)
	}
	return attachInfo
}
