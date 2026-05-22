package oci

import (
	"errors"
	"fmt"

	configstack "micrun/internal/adapters/config/configstack"
	log "micrun/internal/support/logger"
)

// RuntimeStack applies micrun config layers in order (defaults, files, annotations).
type RuntimeStack struct {
	base        *RuntimeConfig
	hostProfile HostProfile
}

func NewRuntimeStackWithHost(hostProfile HostProfile) *RuntimeStack {
	hostProfile = normalizeHostProfile(hostProfile)
	return &RuntimeStack{
		base:        NewRuntimeConfigWithHost(hostProfile),
		hostProfile: hostProfile,
	}
}

// Replace overrides the base config, typically when a specific config file is provided.
func (rs *RuntimeStack) Replace(cfg *RuntimeConfig) {
	if cfg == nil {
		rs.base = NewRuntimeConfigWithHost(rs.hostProfile)
		return
	}
	rs.base = cfg
}

// ApplyMicrunFiles merges micrun config files into the stack.
func (rs *RuntimeStack) ApplyMicrunFiles(files []configstack.MicrunConfigFile) error {
	if rs.base == nil {
		rs.base = NewRuntimeConfigWithHost(rs.hostProfile)
	}
	var applyErr error
	for _, file := range files {
		if err := applyMicrunConfigFile(rs.base, file); err != nil {
			applyErr = errors.Join(applyErr, fmt.Errorf("failed to apply micrun config %s: %w", file.Path, err))
		}
	}
	return applyErr
}

// ApplyAnnotations overlays annotations on top of the stack.
func (rs *RuntimeStack) ApplyAnnotations(annotations map[string]string) {
	if len(annotations) == 0 {
		return
	}
	if rs.base == nil {
		rs.base = NewRuntimeConfigWithHost(rs.hostProfile)
	}
	rs.base.ParseRuntimeConfigFromAnno(annotations)
}

// Config returns the merged runtime config.
func (rs *RuntimeStack) Config() *RuntimeConfig {
	if rs.base == nil {
		rs.base = NewRuntimeConfigWithHost(rs.hostProfile)
	}
	return rs.base
}

func applyMicrunConfigFile(cfg *RuntimeConfig, file configstack.MicrunConfigFile) error {
	switch file.Format {
	case configstack.FormatINI:
		log.Debugf("loading micrun config: %s", file.Path)
		return cfg.ParseRuntimeFromINI(file.Path)
	case configstack.FormatTOML:
		log.Debugf("loading micrun config: %s", file.Path)
		return cfg.ParseRuntimeFromToml(file.Path)
	default:
		return fmt.Errorf("config format %v not supported", file.Format)
	}
}
