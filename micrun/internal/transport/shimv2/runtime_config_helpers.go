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
// s.config is shared across concurrent Create RPCs (pod containers created in
// parallel) and read by recoveryBackend(); the read-modify-write below is
// therefore performed under s.mu. Resolve returns the existing config once it
// is set, so the first Create wins and later ones reuse it.
// applyHostRuntimeConfig (daemon startup) may have pre-set s.config to a
// host-only baseline for recovery; that baseline must not shadow the first
// Create's annotations/options, so it is dropped here and the full stack
// (host files + config_path + annotations) is re-resolved once.
// Resolve/configureRuntimePaths never acquire s.mu themselves, so holding it
// here cannot deadlock.
func loadRuntimeConfig(s *shimService, r ports.TaskCreateRequest, annotations map[string]string) (*oci.RuntimeConfig, error) {
	if s == nil {
		return nil, fmt.Errorf("shim service is nil")
	}
	s.Lock()
	defer s.Unlock()
	current := s.config
	if !s.configFromCreate {
		current = nil
	}
	cfg, err := s.runtimeDeps.runtimeResolver.Resolve(current, r, annotations)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, fmt.Errorf("runtime config resolver returned nil")
	}
	if err := configureRuntimePaths(s.runtimeDeps.containerDeps, cfg.StateDir); err != nil {
		return nil, err
	}
	// The runtime.debug annotation previously had no consumer after landing
	// in RuntimeConfig.Debug; honor it by raising the global level once.
	if cfg.Debug {
		log.ForceDebugLevel()
	}
	s.config = cfg
	s.configFromCreate = true
	log.Debugf("loadRuntimeConfig: config loaded successfully")
	return cfg, nil
}
