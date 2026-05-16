package shim

import (
	"fmt"

	oci "micrun/internal/adapters/config/oci"
	"micrun/internal/ports"
	"micrun/internal/support/fs"
	log "micrun/internal/support/logger"

	"github.com/containerd/cgroups"
)

func setupStateDir(stateDir string) error {
	if err := fs.EnsureDir(stateDir, 0o755); err != nil {
		return fmt.Errorf("failed to create micrun state directory %s: %w", stateDir, err)
	}
	return nil
}

func cgroupV1() (bool, error) {
	if cgroups.Mode() == cgroups.Legacy || cgroups.Mode() == cgroups.Hybrid {
		return true, nil
	}
	if cgroups.Mode() == cgroups.Unified {
		return false, nil
	}
	return false, fmt.Errorf("get unknown cgroup mode")
}

// loadRuntimeConfig loads the runtime configuration from annotations, CRI options, or environment variables.
// NOTE: This function should be called without holding s.mu to avoid deadlock.
// The caller is responsible for holding s.mu if needed for thread safety.
func loadRuntimeConfig(s *shimService, r ports.TaskCreateRequest, annotations map[string]string) (*oci.RuntimeConfig, error) {
	if s == nil {
		return nil, fmt.Errorf("shim service is nil")
	}
	cfg, err := s.runtimeDeps.runtimeResolver.Resolve(s.config, r, annotations)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, fmt.Errorf("runtime config resolver returned nil")
	}
	if err := configureRuntimePaths(s.runtimeDeps.containerDeps, cfg.StateDir); err != nil {
		return nil, err
	}
	s.config = cfg
	log.Debugf("loadRuntimeConfig: config loaded successfully")
	return cfg, nil
}
