package micantainer

import "github.com/opencontainers/runtime-spec/specs-go"

type dummyNetwork struct{}

func (dn *dummyNetwork) NetworkIsCreated() bool {
	return true
}

func (dn *dummyNetwork) NetID() string {
	return "dummy"
}

func (dn *dummyNetwork) NetworkCleanup(id string) error {
	return nil
}

// dummySandboxConfig creates a minimal sandbox config for quick development/test
// Note: Workload resources are calculated dynamically from containers.
func dummySandboxConfig(cid string, spec *specs.Spec) (*SandboxConfig, error) {
	return &SandboxConfig{
		ID:       cid,
		Hostname: spec.Hostname,
		Annotations: map[string]string{
			"org.openeuler.micrun.test": "true",
		},
		ContainerConfigs:   make(map[string]*ContainerConfig),
		SharedMemorySize:   64 * 1024 * 1024, // 64MB
		StaticResourceMgmt: false,
	}, nil
}
