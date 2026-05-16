package container

import "github.com/opencontainers/runtime-spec/specs-go"

const (
	miB                = 1024 * 1024
	maxUint32          = ^uint32(0)
	num2CapRatio int64 = 100
)

func (cfg *ContainerConfig) ensureResources() *specs.LinuxResources {
	if cfg == nil {
		return nil
	}
	if cfg.Resources == nil {
		cfg.Resources = &specs.LinuxResources{}
	}
	return cfg.Resources
}

func (cfg *ContainerConfig) cpuSpec() *specs.LinuxCPU {
	if cfg == nil || cfg.Resources == nil {
		return nil
	}
	return cfg.Resources.CPU
}

func (cfg *ContainerConfig) memorySpec() *specs.LinuxMemory {
	if cfg == nil || cfg.Resources == nil {
		return nil
	}
	return cfg.Resources.Memory
}

func cloneLinuxCPU(src *specs.LinuxCPU) *specs.LinuxCPU {
	if src == nil {
		return &specs.LinuxCPU{}
	}
	return &specs.LinuxCPU{
		Shares:          copyUint64(src.Shares),
		Quota:           copyInt64(src.Quota),
		Burst:           copyUint64(src.Burst),
		Period:          copyUint64(src.Period),
		RealtimeRuntime: copyInt64(src.RealtimeRuntime),
		RealtimePeriod:  copyUint64(src.RealtimePeriod),
		Cpus:            src.Cpus,
		Mems:            src.Mems,
		Idle:            copyInt64(src.Idle),
	}
}

func cloneLinuxMemory(src *specs.LinuxMemory) *specs.LinuxMemory {
	if src == nil {
		return &specs.LinuxMemory{}
	}

	return &specs.LinuxMemory{
		Limit:            copyInt64(src.Limit),
		Reservation:      copyInt64(src.Reservation),
		Swap:             copyInt64(src.Swap),
		Swappiness:       copyUint64(src.Swappiness),
		DisableOOMKiller: copyBool(src.DisableOOMKiller),
	}
}

func bytesToMiB(value *int64) uint32 {
	if value == nil || *value <= 0 {
		return 0
	}
	mib := *value / miB
	if mib > int64(maxUint32) {
		return maxUint32
	}
	return uint32(mib)
}

func miBToBytes(value uint32) *int64 {
	v := int64(value) * miB
	return &v
}

func requiredCPUCount(capacity uint32) uint32 {
	if capacity == 0 {
		return 1
	}
	return (capacity + 99) / 100
}

func fallbackVCPUCount(configuredVCPUs, cpuCapacity uint32) uint32 {
	if configuredVCPUs > 0 {
		return configuredVCPUs
	}
	return requiredCPUCount(cpuCapacity)
}
