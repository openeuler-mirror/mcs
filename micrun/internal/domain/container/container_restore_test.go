package container

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"micrun/internal/ports"
	er "micrun/internal/support/errors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContainerRestoreNormalizesRuntimeFields(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStateStore()
	deps := testDepsWithStore(store)
	sandbox := &Sandbox{
		ctx:       ctx,
		id:        "sandbox-container-restore",
		stateRepo: stateRepositoryFromStore(store),
		deps:      deps,
	}
	containerID := "container-restore"
	storage := ContainerStorage{
		ID:        containerID,
		SandboxID: sandbox.id,
		State:     ContainerState{State: StateRunning},
		Config: ContainerConfig{
			ID: containerID,
			Rootfs: RootFs{
				Source: "/rootfs",
			},
		},
		Mounts: []Mount{{Source: "/src", Target: "/dst"}},
	}
	payload, err := json.Marshal(storage)
	require.NoError(t, err)
	require.NoError(t, store.Save(ctx, &ports.RuntimeSnapshot{
		Namespace: runtimeStateNamespaceContainer,
		TaskID:    containerSnapshotID("", containerID),
		Data:      payload,
	}))

	container := &Container{
		id:      containerID,
		sandbox: sandbox,
	}

	require.NoError(t, container.RestoreState())
	assert.Equal(t, ctx, container.ctx)
	assert.Equal(t, filepath.Join(sandbox.id, containerID), container.containerPath)
	assert.Equal(t, "/rootfs", container.rootfs.Source)
	assert.Equal(t, []Mount{{Source: "/src", Target: "/dst"}}, container.mounts)
	assert.NotNil(t, container.guestExec)
	assert.NotNil(t, container.exitNotifier)
}

func TestRestoreStateReturnsContainerNotFoundForNilContainer(t *testing.T) {
	var container *Container
	if err := container.RestoreState(); !errors.Is(err, er.ContainerNotFound) {
		t.Fatalf("nil container RestoreState error = %v, want ContainerNotFound", err)
	}
}

func TestApplyRestoredContainerStateRejectsInvalidStorage(t *testing.T) {
	tests := []struct {
		name    string
		storage *ContainerStorage
		want    string
	}{
		{
			name: "nil storage",
			want: "storage",
		},
		{
			name: "container id mismatch",
			storage: &ContainerStorage{
				ID:    "other-container",
				State: ContainerState{State: StateStopped},
				Config: ContainerConfig{
					ID: "other-container",
				},
			},
			want: "container ID mismatch",
		},
		{
			name: "sandbox id mismatch",
			storage: &ContainerStorage{
				ID:        "container1",
				SandboxID: "other-sandbox",
				State:     ContainerState{State: StateStopped},
				Config: ContainerConfig{
					ID: "container1",
				},
			},
			want: "container sandbox ID mismatch",
		},
		{
			name: "config id mismatch",
			storage: &ContainerStorage{
				ID:    "container1",
				State: ContainerState{State: StateStopped},
				Config: ContainerConfig{
					ID: "other-container",
				},
			},
			want: "container config ID mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := &Container{
				id:      "container1",
				sandbox: &Sandbox{id: "sandbox1"},
			}
			err := container.applyRestoredContainerState(tt.storage)
			require.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), tt.want), "restore error = %v", err)
		})
	}
}
