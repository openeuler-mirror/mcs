package container

import (
	"context"
	"fmt"

	log "micrun/internal/support/logger"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// 遵循资源映射规范：
// 1. CPU 资源：Share -> Weight, Quota/Period -> Capacity, cpuset -> CPUS
// 2. 内存资源：limit -> limit, reservation -> min, threshold 单独管理
func (r *ContainerConfig) ParseOCIResourcesWithPolicy(ctx context.Context, spec *specs.Spec, policy ResourcePolicy) error {
	op, err := newResourceOperation(ctx, r, policy)
	if err != nil {
		return err
	}
	if op.skipInfra() {
		return nil
	}
	spec = normalizeResourceSpec(spec)

	r.ensureResources()
	essentialRes := op.policy.PlanEssentialRes(spec)

	if err := r.applyCPUResources(op.ctx, spec, essentialRes, op.policy); err != nil {
		return err
	}

	r.applyMemoryResources(spec)
	return nil
}

func (r *ContainerConfig) applyCPUResources(ctx context.Context, spec *specs.Spec, essentialRes *ResourceChanges, policy ResourcePolicy) error {
	if r == nil {
		return fmt.Errorf("container config is nil")
	}
	r.ensureResources()
	specCPU := ociCPUResources(spec)
	if specCPU == nil && !hasEssentialCPUResources(essentialRes) {
		return nil
	}

	r.Resources.CPU = cloneLinuxCPU(specCPU)

	if essentialRes != nil && essentialRes.VCPU != nil && *essentialRes.VCPU > 0 {
		r.VCPUNum = *essentialRes.VCPU
	}

	if essentialRes != nil && essentialRes.ClientCPUSet != "" {
		r.Resources.CPU.Cpus = essentialRes.ClientCPUSet
	}

	if cpu := r.Resources.CPU; cpu != nil && cpu.Cpus != "" {
		fallbackVCPUs := fallbackVCPUCount(r.VCPUNum, r.cpuCapacity())
		normalized := normalizeCPUSetWithLimit(cpu.Cpus, fallbackVCPUs, policy.HostMaxPhysCPUs(ctx))
		cpu.Cpus = normalized.Mask
		if normalized.ApplyVCPUs {
			r.VCPUNum = normalized.VCPUs
		}
	}

	logCPUResourceSummary(r)
	return nil
}

func ociCPUResources(spec *specs.Spec) *specs.LinuxCPU {
	if spec == nil || spec.Linux == nil || spec.Linux.Resources == nil {
		return nil
	}
	return spec.Linux.Resources.CPU
}

func hasEssentialCPUResources(res *ResourceChanges) bool {
	if res == nil {
		return false
	}
	return (res.VCPU != nil && *res.VCPU > 0) || res.ClientCPUSet != ""
}

func (r *ContainerConfig) applyMemoryResources(spec *specs.Spec) {
	if r == nil {
		return
	}
	r.ensureResources()
	if spec != nil && spec.Linux != nil && spec.Linux.Resources != nil && spec.Linux.Resources.Memory != nil {
		r.Resources.Memory = cloneLinuxMemory(spec.Linux.Resources.Memory)
		return
	}

	log.Debug("No Memory resources specified in OCI spec, using defaults")
}

func logCPUResourceSummary(cfg *ContainerConfig) {
	if cfg == nil {
		log.Debug("skip CPU resource summary for nil container config")
		return
	}
	cpu := cfg.cpuSpec()
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
	`, cfg.cpuCapacity(), sharesVal, cfg.VCPUNum, cpusetVal, cfg.memoryLimitMB())
}

func ValidateResourceLimitsWithPolicy(ctx context.Context, config *ContainerConfig, policy ResourcePolicy) error {
	op, err := newResourceOperation(ctx, config, policy)
	if err != nil {
		return err
	}
	if op.skipInfra() {
		return nil
	}
	config = op.config
	policy = op.policy

	if cpuLimit := config.cpuCapacity(); cpuLimit > 0 {
		systemCPUs := policy.MaxClientCPUs(op.ctx, config.ExclusiveDom0CPU)
		if int(requiredCPUCount(cpuLimit)) > systemCPUs {
			return fmt.Errorf("container CPU limit %d exceeds system CPU count %d", cpuLimit, systemCPUs)
		}
	}

	if limit := config.memoryLimitMB(); limit > 0 {
		_, totalMB := policy.HostMemoryMiB(op.ctx)
		hostMemMB := totalMB
		if hostMemMB == 0 {
			log.Warn("failed to detect host memory, using fallback value: 2 GiB")
			hostMemMB = 2 * 1024
		}
		if limit > hostMemMB {
			return fmt.Errorf("container memory limit %d MiB exceeds system memory %d MiB", limit, hostMemMB)
		}
	}

	return nil
}
