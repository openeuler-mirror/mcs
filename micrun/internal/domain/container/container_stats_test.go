package container

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatsReturnsVCPUStatsError(t *testing.T) {
	expectedErr := errors.New("vcpu stats failed")
	deps := testDepsWithStore(newMemoryStateStore())
	deps.VCPUStats = func(context.Context) (*VCPUUsageInfo, error) {
		return nil, expectedErr
	}

	_, err := (&Container{
		id:        "container-stats-error",
		config:    &ContainerConfig{ID: "container-stats-error"},
		guestExec: recordingGuestExecutor{},
		sandbox: &Sandbox{
			ctx:   context.Background(),
			deps:  deps,
			state: SandboxState{State: StateRunning},
		},
	}).stats(context.Background())

	require.ErrorIs(t, err, expectedErr)
	assert.Contains(t, err.Error(), "read vcpu stats")
}

func TestStatsUsesVCPUStatsForMatchingContainer(t *testing.T) {
	deps := testDepsWithStore(newMemoryStateStore())
	ctx := context.WithValue(context.Background(), struct{}{}, "marker")
	var gotCtx context.Context
	deps.VCPUStats = func(ctx context.Context) (*VCPUUsageInfo, error) {
		gotCtx = ctx
		return &VCPUUsageInfo{
			DomainVCPUMap: map[string][]VCPUUsageEntry{
				"container-stats": {
					{TimeSeconds: 1.25},
					{TimeSeconds: 0.75},
					{TimeSeconds: -10},
				},
				"other": {
					{TimeSeconds: 100},
				},
			},
		}, nil
	}

	stats, err := (&Container{
		id:        "container-stats",
		config:    &ContainerConfig{ID: "container-stats"},
		guestExec: recordingGuestExecutor{},
		sandbox: &Sandbox{
			ctx:   context.Background(),
			deps:  deps,
			state: SandboxState{State: StateRunning},
		},
	}).stats(ctx)

	require.NoError(t, err)
	if gotCtx != ctx {
		t.Fatal("VCPUStats did not receive caller context")
	}
	require.NotNil(t, stats.ResourceStats)
	assert.Equal(t, uint64(2_000_000), stats.ResourceStats.CPUStats.TotalUsage)
}

func TestStatsReportsMissingContainerDependencies(t *testing.T) {
	deps := testDepsWithStore(newMemoryStateStore())
	sandbox := &Sandbox{
		ctx:   context.Background(),
		deps:  deps,
		state: SandboxState{State: StateRunning},
	}

	_, err := (&Container{
		id:        "missing-config",
		guestExec: recordingGuestExecutor{},
		sandbox:   sandbox,
	}).stats(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "container config")

	_, err = (&Container{
		id:      "missing-guest-exec",
		config:  &ContainerConfig{ID: "missing-guest-exec"},
		sandbox: sandbox,
	}).stats(context.Background())
	require.Error(t, err)
	if !strings.Contains(err.Error(), "guest executor") {
		t.Fatalf("stats error = %v, want guest executor error", err)
	}
}

func TestCPUUsageUsecRequiresStatsDependency(t *testing.T) {
	container := &Container{id: "container-stats"}

	_, err := container.cpuUsageUsec(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vcpu stats dependency")

	_, err = container.cpuUsageUsec(context.Background(), &Dependencies{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "vcpu stats dependency")
}
