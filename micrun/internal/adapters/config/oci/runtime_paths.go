package oci

import (
	"strings"

	defs "micrun/internal/support/definitions"
	"micrun/internal/support/fs"
	log "micrun/internal/support/logger"
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
		// Surface the rejection: otherwise an operator who supplies a
		// malformed state_dir gets zero feedback and silently keeps the
		// default, which is hard to diagnose. Mirrors the parseRuntimeBool /
		// parseRuntimeUint32 setters, which log on invalid input.
		log.Warnf("ignoring invalid state_dir %q: %v", trimmed, err)
		return
	}
	r.StateDir = clean
}

// StripHostPathConfig resets host-filesystem path settings (state_dir,
// firmware_path) to defaults, reporting whether any value was stripped. A
// config file referenced through the pod annotation config_path is
// pod-author-controlled input: honoring host-path keys would let an
// unprivileged pod author make the root-privileged shim create directories
// and write state/cache files at arbitrary host locations (state_dir), or
// read arbitrary host files as firmware (firmware_path). Admin-side config
// sources (containerd runtime options, env, default discovery) keep the full
// key set.
func (r *RuntimeConfig) StripHostPathConfig() (stripped bool) {
	if r.StateDir != defaultRuntimeStateDir {
		r.StateDir = defaultRuntimeStateDir
		stripped = true
	}
	if r.DefaultFirmwarePath != "" {
		r.DefaultFirmwarePath = ""
		stripped = true
	}
	return stripped
}
