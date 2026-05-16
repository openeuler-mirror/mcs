package shim

import (
	"context"
	"sync"

	oci "micrun/internal/adapters/config/oci"
	cntr "micrun/internal/domain/container"
	"micrun/internal/support/timex"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
)

const (
	channelSize = 128
	okCode      = 0
	exitCode    = 255
)

var (
	_ taskAPI.TaskService = (*shimService)(nil)
)

// shimService owns shim-side runtime state and delegates RPC behavior to the
// task manager plus auxiliary transport helpers.
type shimService struct {
	sync.Mutex
	id          string
	shimPid     uint32
	namespace   string
	config      *oci.RuntimeConfig
	containers  map[string]*shimContainer
	sandbox     cntr.SandboxTraits
	ctx         context.Context
	events      chan shimEvent
	ec          chan exitEvent
	publisher   shimv2.Publisher
	ss          func()
	runtimeDeps runtimeDependencies
	processID   processIDProvider
	shutdown    shutdownEffects
	now         timex.Clock
	tm          *taskManager
	killedByAPI bool
}
