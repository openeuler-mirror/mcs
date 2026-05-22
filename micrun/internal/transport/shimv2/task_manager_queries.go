package shim

import (
	"context"

	"micrun/internal/support/lockutil"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
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

func (m *taskManager) Pids(ctx context.Context, r *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	if err := requireTransportRequest("pids", r); err != nil {
		return nil, err
	}
	return pidsResponse(m.runtimeShimPID()), nil
}

func (m *taskManager) Stats(ctx context.Context, r *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {
	if err := requireTransportRequest("stats", r); err != nil {
		return nil, err
	}
	source, found := m.metricsSource(r.ID)
	if !found {
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
