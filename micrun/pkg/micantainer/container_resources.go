package micantainer

import (
	"fmt"
	defs "micrun/definitions"
	log "micrun/logger"
	"micrun/pkg/libmica"
	"micrun/pkg/pedestal"

	"github.com/opencontainers/runtime-spec/specs-go"
)

const (
	miB                = 1024 * 1024
	num2CapRatio int64 = 100
)

// 资源映射规范：
// 1. CPU 资源映射：
//    - Container CPU Share (1024:256) -> RTOS Client CPU Weight
//    - Container Quota/Period (1:100) -> RTOS Client CPU Capacity (百分比)
//    - Container cpuset -> RTOS Client CPUS
// 2. 内存资源映射：
//    - Container memory limit -> RTOS Client memory limit
//    - Container memory reservation -> RTOS Client memory min
//    - memoryThreshold 仅在 micaexecutor 中记录，保证 memory threshold >= container memory limit

func (cfg *ContainerConfig) ensureResources() *specs.LinuxResources {
	if cfg == nil {
		return nil
	}
	if cfg.Resources == nil {
		cfg.Resources = &specs.LinuxResources{}
	}
	return cfg.Resources
}

func (cfg *ContainerConfig) ensureCPU() *specs.LinuxCPU {
	res := cfg.ensureResources()
	if res == nil {
		return nil
	}
	if res.CPU == nil {
		res.CPU = &specs.LinuxCPU{}
	}
	return res.CPU
}

func (cfg *ContainerConfig) ensureMemory() *specs.LinuxMemory {
	res := cfg.ensureResources()
	if res == nil {
		return nil
	}
	if res.Memory == nil {
		res.Memory = &specs.LinuxMemory{}
	}
	return res.Memory
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

func (cfg *ContainerConfig) cpuCapacity() uint32 {
	cpu := cfg.cpuSpec()
	if cpu == nil || cpu.Quota == nil || cpu.Period == nil || *cpu.Period == 0 {
		return 0
	}
	if *cpu.Quota <= 0 {
		return 0
	}
	// CPU Quota/Period -> CPU Capacity 映射
	// 转换公式：capacity = (quota × 100) / period
	// 表示占满单核的百分比，100% = 占满一个 vCPU
	capacity := (*cpu.Quota * num2CapRatio) / int64(*cpu.Period)
	if capacity <= 0 {
		return 0
	}
	return uint32(capacity)
}

func (cfg *ContainerConfig) cpuShares() uint64 {
	cpu := cfg.cpuSpec()
	if cpu == nil || cpu.Shares == nil {
		return 0
	}
	// CPU Shares -> CPU Weight 映射
	// 默认值：cgroup 默认 1024，Xen 默认 256
	// 转换比例：1024 : 256 = 4:1
	return *cpu.Shares
}

func (cfg *ContainerConfig) cpuMask() string {
	cpu := cfg.cpuSpec()
	if cpu == nil {
		return ""
	}
	// Container cpuset -> RTOS Client CPUS
	// 格式："0-3" 或 "0,1,3"
	// 作用：硬亲和性，限制客户机只能在指定的 pCPU 上运行
	return cpu.Cpus
}

func (cfg *ContainerConfig) containerMaxMemMB() uint32 {
	// 内存资源映射：
	// 1. Container memory limit -> RTOS Client memory limit
	// 2. Container memory reservation -> RTOS Client memory min
	// 3. memoryThreshold 仅在 micaexecutor 中记录，保证 memory threshold >= container memory limit

	lim := cfg.memoryLimitMB()
	if lim != 0 {
		return lim
	}
	lim = cfg.memoryReservationMB()
	if lim != 0 {
		return lim
	}
	return defs.DefaultMinMemMB
}

func (cfg *ContainerConfig) memoryLimitMB() uint32 {
	return bytesToMiB(cfg.memoryLimitBytes())
}

func (cfg *ContainerConfig) memoryReservationMB() uint32 {
	return bytesToMiB(cfg.memoryReservationBytes())
}

func (cfg *ContainerConfig) memoryLimitBytes() *int64 {
	mem := cfg.memorySpec()
	if mem == nil {
		return nil
	}
	return mem.Limit
}

func (cfg *ContainerConfig) memoryReservationBytes() *int64 {
	mem := cfg.memorySpec()
	if mem == nil {
		return nil
	}
	return mem.Reservation
}

func (cfg *ContainerConfig) setMemoryReservationMB(mb uint32) {
	mem := cfg.ensureMemory()
	if mem == nil {
		return
	}
	mem.Reservation = miBToBytes(mb)
}

// CPUCapacity reports the configured CPU capacity in units of 0.01 CPUs.
// 映射关系：Container Quota/Period (1:100) -> RTOS Client CPU Capacity (百分比)
// 100% 表示占满一个 vCPU
func (cfg *ContainerConfig) CPUCapacity() uint32 {
	return cfg.cpuCapacity()
}

// CPUShares reports the configured CPU shares weight.
// 映射关系：Container CPU Share (1024:256) -> RTOS Client CPU Weight
// 转换比例：1024 (cgroup默认) : 256 (Xen默认) = 4:1
func (cfg *ContainerConfig) CPUShares() uint64 {
	return cfg.cpuShares()
}

// CPUSet returns the configured CPU affinity mask.
// 映射关系：Container cpuset -> RTOS Client CPUS
// 格式："0-3" 或 "0,1,3"，表示硬亲和性设置
func (cfg *ContainerConfig) CPUSet() string {
	return cfg.cpuMask()
}

// MemoryLimitMiB returns the configured memory limit in MiB.
// 映射关系：Container memory limit -> RTOS Client memory limit
// memoryThreshold 仅在 micaexecutor 中记录，保证 memory threshold >= container memory limit
func (cfg *ContainerConfig) MemoryLimitMiB() uint32 {
	return cfg.memoryLimitMB()
}

// MemoryReservationMiB returns the configured memory reservation in MiB.
// 映射关系：Container memory reservation -> RTOS Client memory min
// 保证：memory reservation < memory limit
func (cfg *ContainerConfig) MemoryReservationMiB() uint32 {
	return cfg.memoryReservationMB()
}

// SetMemoryReservationMB records the requested memory reservation.
// 用于设置 RTOS Client memory min，必须小于 memory limit
func (cfg *ContainerConfig) SetMemoryReservationMB(mb uint32) {
	cfg.setMemoryReservationMB(mb)
}

// ParseOCIResources parses both CPU and Memory resource limits from OCI spec in a single pass
// 遵循资源映射规范：
// 1. CPU 资源：Share -> Weight, Quota/Period -> Capacity, cpuset -> CPUS
// 2. 内存资源：limit -> limit, reservation -> min, threshold 单独管理
func (r *ContainerConfig) ParseOCIResources(spec *specs.Spec) error {
	if r.IsInfra {
		return nil
	}

	r.ensureResources()

	essentialRes := pedestal.PlanEssentialResources(spec)

	if spec.Linux != nil && spec.Linux.Resources != nil && spec.Linux.Resources.CPU != nil {
		r.Resources.CPU = cloneLinuxCPU(spec.Linux.Resources.CPU)

		// VCPU 数量策略：默认 VCPU = 1
		// 可通过 vcpu_pcpu_binding 选项启用 VCPU = Size(cpuSetUnion)
		if essentialRes.Vcpu != nil && *essentialRes.Vcpu > 0 {
			r.VCPUNum = *essentialRes.Vcpu
		}

		// Container cpuset -> RTOS Client CPUS
		if essentialRes.ClientCpuSet != "" {
			if cpu := r.Resources.CPU; cpu != nil {
				cpu.Cpus = essentialRes.ClientCpuSet
			}
		}

		// TODO: need to reuse cpuset package function
		if cpu := r.Resources.CPU; cpu != nil && cpu.Cpus != "" {
			cpus, err := libmica.ParseCPUString(cpu.Cpus)
			if err == nil {
				if ok, out := CpusetRangeValid(cpus); !ok {
					valid := make([]int, 0, len(cpus))
					bad := map[int]struct{}{}
					for _, x := range out {
						bad[x] = struct{}{}
					}
					for _, x := range cpus {
						if _, miss := bad[x]; !miss {
							valid = append(valid, x)
						}
					}
					if len(valid) > 0 {
						cpu.Cpus = pedestal.ParseCPUArr(valid)
						r.VCPUNum = uint32(len(valid))
					} else {
						// All invalid; clear cpuset and keep a sane default for VCPUs.
						cpu.Cpus = ""
						r.VCPUNum = 1
					}
				}
			}
		}

		cpu := r.Resources.CPU
		var sharesVal uint64
		var cpusetVal string
		if cpu != nil {
			if cpu.Shares != nil {
				sharesVal = *cpu.Shares
			}
			cpusetVal = cpu.Cpus
		}
		log.Debugf(`
			EssentialResource:
			CpuCapacity = %d (quota/period -> capacity, 100%% = 1 vCPU)
			CpuShares = %d (share -> weight, 1024:256 ratio)
			VCPUNum = %d (default=1，configurable)
			CpusetCpus = %s (hard affinity)
			MemoryLimit = %d MiB
		}
		`, r.cpuCapacity(), sharesVal, r.VCPUNum, cpusetVal, r.memoryLimitMB())
	}

	// 内存资源解析
	// Container memory limit -> RTOS Client memory limit
	// Container memory reservation -> RTOS Client memory min
	if spec.Linux != nil && spec.Linux.Resources != nil && spec.Linux.Resources.Memory != nil {
		r.Resources.Memory = cloneLinuxMemory(spec.Linux.Resources.Memory)
	} else {
		log.Warn("No Memory resources specified in OCI spec")
	}

	return nil
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
	return uint32(*value / miB)
}

func miBToBytes(value uint32) *int64 {
	v := int64(value) * miB
	return &v
}

// validateResourceLimits validates container resource limits against system constraints
func ValidateResourceLimits(config *ContainerConfig) error {
	if config.IsInfra {
		return nil
	}
	// Validate CPU limits
	if cpuLimit := config.cpuCapacity(); cpuLimit > 0 {
		systemCPUs := libmica.MaxClientCPUNum()
		if int(cpuLimit) > systemCPUs {
			return fmt.Errorf("container CPU limit %d exceeds system CPU count %d", cpuLimit, systemCPUs)
		}
	}

	// Validate memory limits
	if limit := config.memoryLimitMB(); limit > 0 {
		mem := pedestal.HostMemoryMiB()
		hostMemMB := mem.TotalMB
		if hostMemMB == 0 {
			log.Warn("Failed to detect host memory, using fallback value: 2 GiB")
			hostMemMB = 2 * 1024 // Fallback to 2GiB when detection fails.
		}
		if limit > hostMemMB {
			return fmt.Errorf("container memory limit %d MiB exceeds system memory %d MiB", limit, hostMemMB)
		}
	}

	return nil
}
