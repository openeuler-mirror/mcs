package oci

import (
	"fmt"

	log "micrun/logger"
	configstack "micrun/pkg/configstack"
)

// RuntimeStack applies micrun config layers in order (defaults, files, annotations).
type RuntimeStack struct {
	base *RuntimeConfig
}

// NewRuntimeStack creates a stack initialized with default runtime config.
func NewRuntimeStack() *RuntimeStack {
	return &RuntimeStack{base: NewRuntimeConfig()}
}

// Replace overrides the base config, typically when a specific config file is provided.
func (rs *RuntimeStack) Replace(cfg *RuntimeConfig) {
	if cfg == nil {
		rs.base = NewRuntimeConfig()
		return
	}
	rs.base = cfg
}

// ApplyMicrunFiles merges micrun config files into the stack.
func (rs *RuntimeStack) ApplyMicrunFiles(files []configstack.MicrunConfigFile) {
	if rs.base == nil {
		rs.base = NewRuntimeConfig()
	}
	for _, file := range files {
		if err := applyMicrunConfigFile(rs.base, file); err != nil {
			log.Warnf("failed to apply micrun config %s: %v", file.Path, err)
		}
	}
}

// ApplyAnnotations overlays annotations on top of the stack.
func (rs *RuntimeStack) ApplyAnnotations(annotations map[string]string) {
	if len(annotations) == 0 {
		return
	}
	if rs.base == nil {
		rs.base = NewRuntimeConfig()
	}
	rs.base.ParseRuntimeConfigFromAnno(annotations)
}

// Config returns the merged runtime config.
func (rs *RuntimeStack) Config() *RuntimeConfig {
	if rs.base == nil {
		rs.base = NewRuntimeConfig()
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
