//go:build debug
// +build debug

package defs

import "os"

const (
	MicaConfDir  = "/etc/mica"
	MicaStateDir = "/run/mica"
	DaemonRoot   = "/run"

	DirMode  = os.FileMode(0700) | os.ModeDir
	FileMode = os.FileMode(0644)
)

const (
	// the external state directory for a container, which containers cached rootfs and serialized states
	MicrunStateDir            = "/tmp/micrun"
	DefaultMicaContainersRoot = MicrunStateDir + "/containers"
	MicrunContainerStateFile  = "state.json"
	SandboxStateFile          = "state.json"
	// directory for sandbox data storage
	SandboxDataDir = MicrunStateDir + "/sandbox"

	// Micrun configuration (INI today, easy to switch to TOML later).
	MicrunConfDir    = "/etc/mica/micrun"
	MicrunConfDropin = MicrunConfDir + "/conf.d"
	// specify
	MicrunConfEnv     = "MICRUN_CONF_FILE"
	MicrunConfDirEnv  = "MICRUN_CONF_DIR"
	DefaultMicrunConf = "micrun.conf"
)
