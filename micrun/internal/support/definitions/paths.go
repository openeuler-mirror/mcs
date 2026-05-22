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
	MicrunStateDir            = "/run/micrun"
	DefaultMicaContainersRoot = "/run/micrun/containers"
	MicrunContainerStateFile  = "state.json"
	SandboxStateFile          = "state.json"
	// directory for sandbox data storage
	SandboxDataDir = "/run/micrun/sandbox"

	// Micrun configuration (INI today, easy to switch to TOML later).
	MicrunConfDir    = "/etc/mica/micrun"
	MicrunConfDropin = MicrunConfDir + "/conf.d"
	// specify
	MicrunConfEnv     = "MICRUN_CONF_FILE"
	MicrunConfDirEnv  = "MICRUN_CONF_DIR"
	DefaultMicrunConf = "micrun.conf"
	MicrunConfFile    = "config.json"

	// Default log configuration path for micrun logger.
	MicrunLogConfigPath = MicrunConfDir + "/" + MicrunConfFile

	// Default log file path for micrun logger debug output.
	MicrunLogFile = "/var/log/mica/mica-runtime.log"

	// Additional path constants
	MicadPidFile      = "/run/micad.pid"                                // PID file for micad daemon
	ContainerdTaskDir = "/run/containerd/io.containerd.runtime.v2.task" // Containerd task directory
	BackupDirName     = "backup"                                        // Backup directory name (relative to MicrunStateDir)
)
