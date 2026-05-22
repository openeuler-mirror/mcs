package container

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"micrun/internal/ports"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSandboxRestoreNormalizesRuntimeFields(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStateStore()
	deps := testDepsWithStore(store)
	sandboxID := "sandbox-restore-normalized"
	storage := SandboxStorage{
		ID:    sandboxID,
		State: SandboxState{State: StateStopped, Ped: "xen"},
		Config: SandboxConfig{
			ID: sandboxID,
		},
		Network: NetworkConfig{
			NetworkID: "netns",
		},
	}
	payload, err := json.Marshal(storage)
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, &ports.RuntimeSnapshot{
		Namespace: runtimeStateNamespaceSandbox,
		TaskID:    sandboxSnapshotID(sandboxID),
		Data:      payload,
	}))

	guestCtl := &countingGuestControl{}
	hypervisor := &fakeHypervisorControl{name: "restore"}
	sandbox := &Sandbox{
		ctx:               ctx,
		id:                sandboxID,
		stateRepo:         stateRepositoryFromStore(store),
		deps:              deps,
		guestControl:      guestCtl,
		hypervisorControl: hypervisor,
	}

	require.NoError(t, sandbox.restore())
	require.NotNil(t, sandbox.config)
	assert.Same(t, store, sandbox.config.StateStore)
	assert.Same(t, deps, sandbox.config.Dependencies)
	assert.Same(t, guestCtl, sandbox.config.GuestControl)
	assert.Same(t, hypervisor, sandbox.config.HypervisorControl)
	assert.NotNil(t, sandbox.config.ContainerConfigs)
	assert.NotNil(t, sandbox.containers)
	assert.NotNil(t, sandbox.resManager.ContainerCPUSet)
	assert.NotNil(t, sandbox.resManager.ContainerVCPUs)
	assert.NotNil(t, sandbox.wg)

	network, ok := sandbox.network.(*NetworkConfig)
	require.True(t, ok, "network should be restored as *NetworkConfig")
	assert.Same(t, &sandbox.config.NetworkConfig, network)
	assert.Equal(t, "netns", sandbox.config.NetworkConfig.NetworkID)
}

func TestSandboxRestoreRejectsConfigIDMismatch(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStateStore()
	deps := testDepsWithStore(store)
	sandboxID := "sandbox-restore-mismatch"
	storage := SandboxStorage{
		ID:    sandboxID,
		State: SandboxState{State: StateStopped, Ped: "xen"},
		Config: SandboxConfig{
			ID: "other-sandbox",
		},
	}
	payload, err := json.Marshal(storage)
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, &ports.RuntimeSnapshot{
		Namespace: runtimeStateNamespaceSandbox,
		TaskID:    sandboxSnapshotID(sandboxID),
		Data:      payload,
	}))

	sandbox := &Sandbox{
		ctx:       ctx,
		id:        sandboxID,
		stateRepo: stateRepositoryFromStore(store),
		deps:      deps,
	}

	err = sandbox.restore()
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "sandbox config ID mismatch"), "restore error = %v", err)
}
