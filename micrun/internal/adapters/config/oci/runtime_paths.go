package oci

import (
	"strings"

	defs "micrun/internal/support/definitions"
	"micrun/internal/support/fs"
)

const defaultRuntimeStateDir = defs.MicrunStateDir

type RuntimePathConfig struct {
	StateDir string
}

func defaultRuntimePathConfig() RuntimePathConfig {
	return RuntimePathConfig{
		StateDir: defaultRuntimeStateDir,
	}
}

func (r *RuntimeConfig) SetStateDir(stateDir string) {
	trimmed := strings.TrimSpace(stateDir)
	if trimmed == "" {
		return
	}
	clean, err := fs.CleanAbsolutePath(trimmed)
	if err != nil {
		return
	}
	r.StateDir = clean
}
