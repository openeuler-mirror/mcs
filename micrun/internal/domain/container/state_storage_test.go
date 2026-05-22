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

func TestContainerStorageFromContainerCopiesPersistenceFields(t *testing.T) {
	sandbox := &Sandbox{id: "sandbox1"}
	container := &Container{
		id:            "container1",
		sandbox:       sandbox,
		config:        &ContainerConfig{ID: "container1"},
		state:         ContainerState{State: StateReady},
		mounts:        []Mount{{Target: "/data"}},
		containerPath: "sandbox1/container1",
	}

	got := containerStorageFromContainer(container)

	if got.ID != container.id || got.SandboxID != sandbox.id || got.ContainerPath != container.containerPath {
		t.Fatalf("container storage identity = %+v", got)
	}
	if got.Config.ID != container.config.ID || got.State.State != container.state.State {
		t.Fatalf("container storage state/config = %+v", got)
	}
	if len(got.Mounts) != 1 || got.Mounts[0].Target != "/data" {
		t.Fatalf("container storage mounts = %+v", got.Mounts)
	}
}
