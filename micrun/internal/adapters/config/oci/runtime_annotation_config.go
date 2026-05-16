package oci

import (
	"strings"

	ann "micrun/internal/support/annotations"
)

var runtimeAnnotationSetters = map[string]func(*RuntimeConfig, string){
	ann.RuntimeDebug:              (*RuntimeConfig).SetDebug,
	ann.RuntimeMaxContainerCPUs:   (*RuntimeConfig).SetMaxContainerVCPUs,
	ann.RuntimeMaxContainerMemory: (*RuntimeConfig).SetMaxContainerMemMB,
	ann.RuntimePauseImage:         (*RuntimeConfig).SetPauseImage,
	ann.RuntimeExclusiveDom0CPU:   (*RuntimeConfig).SetExclusiveDom0CPU,
}

// ParseRuntimeConfigFromAnno parses runtime configuration from annotations.
// Annotations hold highest priority for values.
func (cfg *RuntimeConfig) ParseRuntimeConfigFromAnno(annotations map[string]string) *RuntimeConfig {
	for key, value := range annotations {
		trimmed := strings.TrimSpace(value)
		if !strings.HasPrefix(key, ann.MicrunAnnotationPrefix) || trimmed == "" {
			continue
		}

		if apply, ok := runtimeAnnotationSetters[key]; ok {
			apply(cfg, trimmed)
		}
	}

	return cfg
}
