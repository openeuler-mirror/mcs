package annotations

// OCI and runtime annotations.
const (
	// MicrunAnnotationPrefix is the prefix for all micrun-specific annotations.
	MicrunAnnotationPrefix = "org.openeuler.micrun."
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

// Configuration for mica clients, passed to the sandbox container.
// Micad daemon configuration is process-wide and must not be modeled as a
// per-container annotation here.
const (
	// OSAnnotation specifies the client OS type. Corresponds to ini config keys in [Mica] section of client.conf.
	OSAnnotation = ContainerPrefix + "os"
	// FirmwarePathAnno specifies the relative path to the firmware mica required, in the bundle.
	FirmwarePathAnno = ContainerPrefix + "firmware_path"
	// FirmwareHash is the sha-256 hash of the firmware.
	FirmwareHash = ContainerPrefix + "firmware_hash"

	// AutoClose controls whether the container automatically closes after timeout.
	// Priority: auto_close_timeout > auto_close > default
	// If set to false, auto-close is disabled unless auto_close_timeout is set.
	AutoClose = ContainerPrefix + "auto_close"

	// AutoCloseTimeout specifies the duration before auto-close triggers.
	// Has HIGHER priority than auto_close. If set, auto-close is enabled
	// regardless of auto_close value.
	// Format: duration string (e.g., "60s", "5m") or integer seconds (e.g., "60")
	// Special values:
	//   - "0" or "0s" = disabled (infinite connection, no timeout)
	//   - negative values = invalid (error, falls back to default)
	// Default: "30s" if not specified
	AutoCloseTimeout = ContainerPrefix + "auto_close_timeout"

	// OldAutoCloseTimeout is the deprecated auto-close timeout key.
	OldAutoCloseTimeout = ContainerPrefix + "auto_disconnect_timeout"

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
	// Experimental enables experimental features.
	Experimental = RuntimePrefix + "experimental"
	// PipeSize specifies the pipe size for IO.
	PipeSize = RuntimePrefix + "pipe_size"
	// RuntimeDebug enables debug mode for the runtime.
	RuntimeDebug = RuntimePrefix + "debug"
	// RuntimeMaxContainerCPUs specifies the maximum container CPU count.
	RuntimeMaxContainerCPUs = RuntimePrefix + "max_container_cpus"
	// RuntimeMaxContainerMemory specifies the maximum container memory.
	RuntimeMaxContainerMemory = RuntimePrefix + "max_container_memory"
	// RuntimePauseImage overrides the pause image used by the runtime.
	RuntimePauseImage = RuntimePrefix + "pause"
	// RuntimeExclusiveDom0CPU toggles whether Dom0 CPUs are kept exclusive (Xen).
	RuntimeExclusiveDom0CPU = RuntimePrefix + "exclusive_dom0_cpu"
	// RuntimeEnableVCPUsPinning toggles VCPU pinning for the sandbox.
	RuntimeEnableVCPUsPinning = RuntimePrefix + "enable_vcpus_pinning"
	// RuntimeStaticResource toggles static resource management for the sandbox.
	RuntimeStaticResource = RuntimePrefix + "static_resource"
	// RuntimeHugePageEnable toggles huge page support for the sandbox.
	RuntimeHugePageEnable = RuntimePrefix + "hugepage_enable"
	// VCPUBinding is a compatibility alias for RuntimeEnableVCPUsPinning.
	VCPUBinding = RuntimePrefix + "vcpu_pcpu_binding"
)
