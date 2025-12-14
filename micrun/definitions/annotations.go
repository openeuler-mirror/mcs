package defs

// TODO: Migrate annotations.go to package annotations
// OCI and runtime annotations.
const (
	// MicrunAnnotationPrefix is the prefix for all micran-specific annotations.
	MicrunAnnotationPrefix = "org.openeuler.micrun." // For runtime-level configuration.
	// PedPrefix is the prefix for pedestal-related configurations.
	PedPrefix = MicrunAnnotationPrefix + "ped."
	// RuntimePrefix is the prefix for runtime-related configurations.
	RuntimePrefix = MicrunAnnotationPrefix + "runtime."
	// ContainerPrefix is the prefix for container-related configurations.
	ContainerPrefix = MicrunAnnotationPrefix + "container."
	// CompatPrefix is the prefix for compatibility-related configurations.
	CompatPrefix = MicrunAnnotationPrefix + "compatibility."

	// BundlePathKey is the annotation key for the OCI configuration file path.
	BundlePathKey = MicrunAnnotationPrefix + "pkg.oci.bundle_path"
	// ContainerTypeKey is the annotation key for the container type.
	ContainerTypeKey = MicrunAnnotationPrefix + "pkg.oci.container_type"
	// SandboxConfigPathKey is the annotation key for the sandbox configuration path.
	SandboxConfigPathKey = MicrunAnnotationPrefix + "config_path"
)

// Pedestal configurations.
const (
// Basically about Xen.
)

// Configuration for mica clients, passed to the sandbox container.
// NOTICE: Micad is shared for all micrans, which means that micad can not be configured differently.
// Hence the freedom degree is limited.
// TODO: An idea, support dynamic configuration loader module for micad.
const (

	// OSAnnotation specifies the client OS type. Corresponds to ini config keys in [Mica] section of client.conf.
	OSAnnotation = ContainerPrefix + "os"
	// FirmwarePathAnno specifies the relative path to the firmware mica required, in the bundle.
	FirmwarePathAnno = ContainerPrefix + "firmware_path"
	// FirmwareHash is the sha-256 hash of the firmware.
	FirmwareHash = ContainerPrefix + "firmware_hash"
	// Some rtos may not support in-client shutdown well, so micrun add timeout autodisconnect
	AutoClose = ContainerPrefix + "auto_close"
	// Default to be 30 seconds, future: read this default timeout from config file
	AutoCloseTimeout = ContainerPrefix + "auto_disconnect_timeout"
	// Pedtype specifies the pedestal type.
	Pedtype = PedPrefix + "pedestal"
	// PedCompat specifies compatibility options: format "^versionX" (deprecated, use CompatPrefix directly)
	PedCompat = PedPrefix + "compatibility" // DEPRECATED: Use CompatPrefix instead
	// NetPlaceholder is a placeholder for network configuration.
	NetPlaceholder = PedPrefix + "net_placeholder"
	PedestalConf   = PedPrefix + "conf"
)

// Container-specific runtime settings.
const (
	// ContainerMinMemMB specifies the initial memory (MiB) assigned to the client at boot.
	// This differs from the max memory limit (Memory/MaxMemMB) that may come from OCI.
	ContainerMinMemMB = ContainerPrefix + "min_memory_mb"
	// ContainerMaxVcpuNum allows overriding the runtime max_vcpu_num for micad create messages.
	ContainerMaxVcpuNum = ContainerPrefix + "max_vcpu_num"
)

const (
	// DisableNewNetNs disables the creation of a new network namespace.
	DisableNewNetNs = RuntimePrefix + "disable_new_netns"
	// Experiemental enables experimental features.
	Experiemental = RuntimePrefix + "experimental"
	// PipeSize specifies the pipe size for IO.
	PipeSize = RuntimePrefix + "pipe_size"
	// RuntimeDebug enables debug mode for the runtime.
	RuntimeDebug = RuntimePrefix + "debug"
	// RuntimeExclusiveDom0CPU toggles whether Dom0 CPUs are kept exclusive (Xen).
	RuntimeExclusiveDom0CPU = RuntimePrefix + "exclusive_dom0_cpu"
	// TODO: implement the logic binding vpuc number and size(cpusetUnion of pcpu)
	VCPUBinding = RuntimePrefix + "vcpu_pcpu_binding"
)

const (
	// TODO: We need a special Pause image.
	// PauseImage is the image used for pausing a container.
	PauseImage = "registry.k8s.io/pause"
	// SandboxVersion is the version of the sandbox.
	SandboxVersion = 1
)
