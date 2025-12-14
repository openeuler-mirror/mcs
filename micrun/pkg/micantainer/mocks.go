package micantainer

import "github.com/opencontainers/runtime-spec/specs-go"

type DummyNetwork struct{}

func (dn *DummyNetwork) NetworkIsCreated() bool {
	return true
}

func (dn *DummyNetwork) NetID() string {
	return "dummy"
}

func (dn *DummyNetwork) NetworkCleanup(id string) error {
	return nil
}

// DummySandboxConfig creates a minimal sandbox config for quick development/test
// Note: Workload resources are calculated dynamically from containers.
func DummySandboxConfig(cid string, spec *specs.Spec) (*SandboxConfig, error) {
	return &SandboxConfig{
		ID:       cid,
		Hostname: spec.Hostname,
		Annotations: map[string]string{
			"org.openeuler.micran.test": "true",
		},
		ContainerConfigs:   make(map[string]*ContainerConfig),
		SharedMemorySize:   64 * 1024 * 1024, // 64MB
		StaticResourceMgmt: false,
	}, nil
}
