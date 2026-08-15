package shim

import (
	"context"
	"sync"
	"sync/atomic"

	oci "micrun/internal/adapters/config/oci"
	cntr "micrun/internal/domain/container"
	"micrun/internal/support/timex"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
)

const (
	channelSize = 128
)

var (
	_ taskAPI.TaskService = (*shimService)(nil)
)

// shimService owns shim-side runtime state and delegates RPC behavior to the
// task manager plus auxiliary transport helpers.
type shimService struct {
	sync.Mutex
	sandboxMu sync.RWMutex
	id        string
	shimPid   uint32
	namespace string
	config    *oci.RuntimeConfig
	// configFromCreate marks s.config as resolved by a Create RPC (with the
	// pod's annotations/options applied). applyHostRuntimeConfig leaves it
	// false: its host-only baseline exists for recovery and must not stop
	// the first Create from overlaying per-pod runtime settings.
	configFromCreate bool
	containers       map[string]*shimContainer
	sandbox          cntr.SandboxTraits
	ctx              context.Context
	events           chan shimEvent
	ec               chan exitEvent
	ss               func()
	runtimeDeps      runtimeDependencies
	processID        processIDProvider
	shutdown         shutdownEffects
	now              timex.Clock
	tm               *taskManager
	killedByAPI      atomic.Bool
}
