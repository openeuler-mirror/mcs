package shim

import (
	"context"

	"micrun/internal/support/lockutil"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/errdefs"
	ptypes "github.com/containerd/containerd/protobuf/types"
)

func (m *taskManager) State(ctx context.Context, r *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {
	if err := requireTransportRequest("state", r); err != nil {
		return nil, err
	}
	out, err := m.service.State(ctx, m.query, stateInputFromTransport(r))
	if err != nil {
		return nil, grpcExecAwareRequestError(r, err)
	}
	return stateTaskResponse(out), nil
}

// taskPresent is the only sanctioned way for query RPCs to check
// task presence: it takes the runtime lock around the containers-map
// read, matching the locked Create/Delete writers. An unlocked read here
// races a concurrent map write and fatals the shim.
func (m *taskManager) taskPresent(id string) bool {
	return lockutil.WithLockValue(m.metrics, func() bool {
		return m.metrics.hasShimTask(id)
	})
}

func (m *taskManager) Pids(ctx context.Context, r *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	if err := requireTransportRequest("pids", r); err != nil {
		return nil, err
	}
	// Match the containerd contract: an unknown (e.g. already deleted) task
	// id must surface NotFound, not a shim-PID success that hides the fact.
	if !m.taskPresent(r.ID) {
		return nil, errdefs.ErrNotFound
	}
	return pidsResponse(m.runtimeShimPID()), nil
}

func (m *taskManager) Stats(ctx context.Context, r *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {
	if err := requireTransportRequest("stats", r); err != nil {
		return nil, err
	}
	source, found := m.metricsSource(r.ID)
	if !found {
		// Unknown task id: NotFound instead of empty-but-success metrics.
		if !m.taskPresent(r.ID) {
			return nil, errdefs.ErrNotFound
		}
		return statsResponse(m.metrics.EmptyMetrics()), nil
	}
	return statsResponse(m.metricsOrEmpty(ctx, r.ID, source)), nil
}

func (m *taskManager) metricsSource(id string) (metricsSource, bool) {
	shimPID := m.runtimeShimPID()
	snapshot := lockutil.WithLockValue(m.metrics, func() struct {
		source metricsSource
		found  bool
	} {
		return struct {
			source metricsSource
			found  bool
		}{
			source: metricsSourceFromRuntime(m.metrics, shimPID),
			found:  m.metrics.hasShimTask(id),
		}
	})
	if !snapshot.found {
		return metricsSource{}, false
	}
	return snapshot.source, true
}

func (m *taskManager) metricsOrEmpty(ctx context.Context, id string, source metricsSource) *ptypes.Any {
	data, err := marshalMetrics(ctx, source, id)
	if err != nil || data == nil {
		return m.metrics.EmptyMetrics()
	}
	return data
}

func (m *taskManager) Connect(ctx context.Context, r *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {
	if err := requireTransportRequest("connect", r); err != nil {
		return nil, err
	}
	if !m.taskPresent(r.ID) {
		return nil, errdefs.ErrNotFound
	}
	return connectResponse(m.runtimeShimPID()), nil
}

func (m *taskManager) Shutdown(ctx context.Context, r *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {
	if err := requireTransportRequest("shutdown", r); err != nil {
		return nil, err
	}
	if m.runtimeHasContainers() {
		return emptyResponse, nil
	}
	m.shutdown.runShutdownEffects()
	return emptyResponse, nil
}

func (m *taskManager) runtimeShimPID() uint32 {
	return lockutil.WithLockValue(m.process, func() uint32 {
		return m.process.ShimPID()
	})
}

func (m *taskManager) runtimeHasContainers() bool {
	return lockutil.WithLockValue(m.taskPresence, func() bool {
		return m.taskPresence.hasShimTasks()
	})
}
