package container

import "testing"

func TestSandboxStorageFromSandboxUsesRuntimeNetwork(t *testing.T) {
	network := &NetworkConfig{NetworkID: "runtime-net", NetworkCreated: true, HolderPid: 123}
	sandbox := &Sandbox{
		id:      "sandbox1",
		config:  &SandboxConfig{ID: "sandbox1"},
		state:   SandboxState{State: StateRunning},
		network: network,
	}

	got := sandboxStorageFromSandbox(sandbox, 10, 20)

	if got.ID != "sandbox1" || got.CreatedAt != 10 || got.ShimPID != 20 {
		t.Fatalf("sandbox storage metadata = %+v", got)
	}
	if got.Network != *network {
		t.Fatalf("network storage = %+v, want %+v", got.Network, *network)
	}
}

func TestSandboxStorageFromSandboxFallsBackToConfigNetwork(t *testing.T) {
	want := NetworkConfig{NetworkID: "config-net", NetworkCreated: true, HolderPid: 456}
	sandbox := &Sandbox{
		id:     "sandbox1",
		config: &SandboxConfig{ID: "sandbox1", NetworkConfig: want},
	}

	got := sandboxStorageFromSandbox(sandbox, 0, 0)

	if got.Network != want {
		t.Fatalf("network storage = %+v, want %+v", got.Network, want)
	}
}

// The combined sandbox document embeds each container's runtime record; this
// covers the same persistence fields the removed per-container writer used to
// carry (state, mounts, container path) plus the config via ContainerConfigs.
func TestSandboxStorageFromSandboxEmbedsContainerRecords(t *testing.T) {
	sandbox := &Sandbox{
		id:     "sandbox1",
		config: &SandboxConfig{ID: "sandbox1"},
		state:  SandboxState{State: StateRunning},
	}
	container := &Container{
		id:            "container1",
		sandbox:       sandbox,
		config:        &ContainerConfig{ID: "container1"},
		state:         ContainerState{State: StateReady},
		mounts:        []Mount{{Target: "/data"}},
		containerPath: "sandbox1/container1",
	}
	sandbox.containers = map[string]*Container{container.id: container}

	got := sandboxStorageFromSandbox(sandbox, 0, 0)

	record, ok := got.Containers[container.id]
	if !ok {
		t.Fatalf("container record missing from sandbox storage: %+v", got.Containers)
	}
	if record.State.State != StateReady || record.ContainerPath != container.containerPath {
		t.Fatalf("container record = %+v", record)
	}
	if len(record.Mounts) != 1 || record.Mounts[0].Target != "/data" {
		t.Fatalf("container record mounts = %+v", record.Mounts)
	}
}
