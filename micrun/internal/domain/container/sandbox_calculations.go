package container

import (
	"context"
	"fmt"

	"github.com/opencontainers/runtime-spec/specs-go"

	"micrun/internal/support/cpuset"
	log "micrun/internal/support/logger"
	"micrun/internal/support/sys"
)

func calculateSandboxVCPUs(ctx context.Context, s *Sandbox) (uint32, error) {
	if s == nil || s.config == nil {
		return 0, fmt.Errorf("sandbox or sandbox config is nil")
	}

	total := uint32(0)
	for id, cc := range s.config.ContainerConfigs {
		if cc == nil {
			return 0, fmt.Errorf("container config %q is nil", id)
		}
		active, err := s.activeContainer(ctx, cc.ID)
		if err != nil {
			return 0, err
		}
		if cc.IsInfra || !active {
			continue
		}
		total += containerVCPUs(cc)
	}
	return total, nil
}

func containerVCPUs(cc *ContainerConfig) uint32 {
	if cc == nil {
		return 0
	}
	if cc.VCPUNum > 0 {
		return cc.VCPUNum
	}
	if cc.Resources != nil && cc.Resources.CPU != nil {
		if v := vcpusFromQuota(cc.Resources.CPU); v > 0 {
			return v
		}
		if v := vcpusFromCPUSet(cc.Resources.CPU.Cpus); v > 0 {
			return v
		}
	}
	return 1
}

func vcpusFromQuota(cpu *specs.LinuxCPU) uint32 {
	if cpu.Period == nil || cpu.Quota == nil || *cpu.Period == 0 {
		return 0
	}
	m := sys.CalculateMilliCPUs(*cpu.Quota, *cpu.Period)
	return sys.CalculateVCpusFromMilliCpus(m)
}

func vcpusFromCPUSet(cpuStr string) uint32 {
	if cpuStr == "" {
		return 0
	}
	set, err := cpuset.Parse(cpuStr)
	if err != nil {
		return 0
	}
	return uint32(set.Size())
}

func calculateSandboxMemory(ctx context.Context, s *Sandbox) (uint64, error) {
	if s == nil || s.config == nil {
		return 0, fmt.Errorf("sandbox or sandbox config is nil")
	}

	memorySandbox := uint64(0)
	for id, cc := range s.config.ContainerConfigs {
		if cc == nil {
			return 0, fmt.Errorf("container config %q is nil", id)
		}
		active, err := s.activeContainer(ctx, cc.ID)
		if err != nil {
			return 0, err
		}
		if cc.IsInfra || !active {
			continue
		}
		memorySandbox += containerMemory(cc, s.config.HugePageSupport)
	}
	return memorySandbox, nil
}

func containerMemory(cc *ContainerConfig, hugePageSupport bool) uint64 {
	if cc == nil {
		return 0
	}
	if cc.Resources == nil || cc.Resources.Memory == nil {
		return 0
	}
	var total uint64
	m := cc.Resources.Memory
	if m.Limit != nil && *m.Limit > 0 {
		limitMiB := uint64(*m.Limit >> 20)
		total += limitMiB
		log.Debugf("sandbox memory limit + %d MiB", limitMiB)
	}
	if hugePageSupport {
		for _, lim := range cc.Resources.HugepageLimits {
			hpMiB := lim.Limit >> 20
			log.Debugf("sandbox hugepage limit + %d MiB (%s)", hpMiB, lim.Pagesize)
			total += hpMiB
		}
	}
	return total
}

func cpusetRangeValidWithLimit(sortedCpuList []int, maxCpus uint32) (bool, []int) {
	if maxCpus == 0 {
		return true, nil
	}
	outrange := []int{}

	for _, cpu := range sortedCpuList {
		if cpu >= int(maxCpus) {
			outrange = append(outrange, cpu)
		}
	}

	if len(outrange) > 0 {
		log.Warnf("cpuset range is out of machine max cpu: %v", outrange)
		return false, outrange
	}

	return true, outrange
}
