package oci

// Configuration keys for runtime settings.
const (
	KeyStaticResource   = "static_resource"      // default=true
	KeyDebug            = "debug"                // default=false
	KeyStateDir         = "state_dir"            // default=defs.MicrunStateDir
	KeyPauseImg         = "pause_image"          // default=defs.DefaultPauseImage
	KeyMaxContainerVCPU = "max_container_vcpu"   // default=0, unlimited
	KeySandboxMinVCPU   = "sandbox_minimum_vcpu" // default=1
	KeyHugePage         = "hugepage_enable"      // only for Xen; default=false
	KeyExclusiveDom0CPU = "exclusive_dom0_cpu"   // default=false, reserve Dom0 CPUs
	KeyMinMemory        = "container_minmem"     // default base memory for container
	KeyMaxMemory        = "container_maxmem"     // default max memory for container
	KeyDefaultFirmware  = "firmware_path"        // default firmware path when annotation not set
	KeySharedCPUPool    = "shared_cpu_pool"      // default=false, shared CPU pool for Xen cpupool management
)

var runtimeConfigKeys = []string{
	KeyStaticResource,
	KeyDebug,
	KeyStateDir,
	KeyPauseImg,
	KeyMaxContainerVCPU,
	KeySandboxMinVCPU,
	KeyHugePage,
	KeyExclusiveDom0CPU,
	KeyMaxMemory,
	KeyMinMemory,
	KeyDefaultFirmware,
	KeySharedCPUPool,
}

type runtimeConfigSetter struct {
	key   string
	apply func(*RuntimeConfig, string)
}

var runtimeConfigSetters = []runtimeConfigSetter{
	{key: KeyStaticResource, apply: (*RuntimeConfig).SetStaticResourceManagement},
	{key: KeyDebug, apply: (*RuntimeConfig).SetDebug},
	{key: KeyStateDir, apply: (*RuntimeConfig).SetStateDir},
	{key: KeyPauseImg, apply: (*RuntimeConfig).SetPauseImage},
	{key: KeyMaxContainerVCPU, apply: (*RuntimeConfig).SetMaxContainerVCPUs},
	{key: KeySandboxMinVCPU, apply: (*RuntimeConfig).SetMiniVCPUNum},
	{key: KeyHugePage, apply: (*RuntimeConfig).SetHugePageSupport},
	{key: KeyExclusiveDom0CPU, apply: (*RuntimeConfig).SetExclusiveDom0CPU},
	{key: KeyMaxMemory, apply: (*RuntimeConfig).SetMaxContainerMemMB},
	{key: KeyMinMemory, apply: (*RuntimeConfig).SetMinContainerMemMB},
	{key: KeyDefaultFirmware, apply: (*RuntimeConfig).SetDefaultFirmwarePath},
	{key: KeySharedCPUPool, apply: (*RuntimeConfig).SetSharedCPUPool},
}

func (r *RuntimeConfig) applyRawConfig(raw map[string]string) {
	for _, setter := range runtimeConfigSetters {
		value, ok := raw[setter.key]
		if !ok {
			continue
		}
		setter.apply(r, value)
	}
}
