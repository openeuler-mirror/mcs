package container

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A sandbox persisted as STOPPED (pod stopped, Remove not yet arrived) must
// recover as STOPPED. createSandbox used to flip it to ready, which made the
// persisted document forever "ready": the same-id recreate path then failed
// with AlreadyExists instead of deleting the stale entry, and notOperational
// stopped fencing late Creates against a sandbox kubelet considers stopped.
func TestRebuildSandboxPreservesStoppedState(t *testing.T) {
	store := newMemoryStateStore()
	deps := testDepsWithStore(store)
	repo := stateRepositoryFromStore(store)

	ss := &SandboxStorage{
		ID: "sandbox-stopped-recover",
		Config: SandboxConfig{
			ID:               "sandbox-stopped-recover",
			ContainerConfigs: map[string]*ContainerConfig{},
		},
		State: SandboxState{State: StateStopped},
	}
	// Persist first: the real recovery flow loads the document from disk and
	// createSandbox's restore() reads it back from the store.
	require.NoError(t, repo.SaveSandboxStorage(context.Background(), ss))

	sandbox, err := rebuildSandbox(context.Background(), ss, nil, deps, repo)
	require.NoError(t, err)
	assert.Equal(t, StateStopped, sandbox.GetState())

	// The recovery refresh must not flip the persisted state either.
	loaded, err := repo.LoadSandbox(context.Background(), ss.ID)
	require.NoError(t, err)
	assert.Equal(t, StateStopped, loaded.State.State)
}
