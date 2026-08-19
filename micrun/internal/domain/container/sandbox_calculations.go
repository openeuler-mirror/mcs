package container

import (
	"context"
	"fmt"

	"github.com/opencontainers/runtime-spec/specs-go"

	"micrun/internal/support/cpuset"
	log "micrun/internal/support/logger"
	"micrun/internal/support/sys"
)

// cfgVCPU is a lock-free snapshot of the fields containerVCPUs reads, taken
// under containersLock so the calculation does not race with a concurrent
// UpdateContainer/setVcpuAffinity writing the same *ContainerConfig.
type cfgVCPU struct {
	id      string
	isInfra bool
	vcpuNum uint32
	cpu     *specs.LinuxCPU
}

func calculateSandboxVCPUs(ctx context.Context, s *Sandbox) (uint32, error) {
	if s == nil || s.config == nil {
		return 0, fmt.Errorf("sandbox or sandbox config is nil")
	}

	// Snapshot the relevant config fields under RLock, then release before
	// calling activeContainer (which takes its own RLock). Holding RLock
	// across activeContainer would deadlock if a writer is waiting, because
	// Go's RWMutex blocks new RLock callers while a Lock is queued. Copying
	// the fields (not just the pointer) avoids racing with a concurrent
	// writer that reassigns Resources.CPU or mutates VCPUNum.
	s.containersLock.RLock()
	snapshots := make([]cfgVCPU, 0, len(s.config.ContainerConfigs))
	for id, cc := range s.config.ContainerConfigs {
		if cc == nil {
			s.containersLock.RUnlock()
			return 0, fmt.Errorf("container config %q is nil", id)
		}
		snap := cfgVCPU{id: cc.ID, isInfra: cc.IsInfra, vcpuNum: cc.VCPUNum}
		if cc.Resources != nil {
			snap.cpu = cloneLinuxCPU(cc.Resources.CPU)
		}
		snapshots = append(snapshots, snap)
	}
	s.containersLock.RUnlock()

	total := uint32(0)
	for _, snap := range snapshots {
		active, err := s.activeContainer(ctx, snap.id)
		if err != nil {
			return 0, err
		}
		if snap.isInfra || !active {
			continue
		}
		total += snap.vcpus()
	}
	return total, nil
}

func (s cfgVCPU) vcpus() uint32 {
	if s.vcpuNum > 0 {
		return s.vcpuNum
	}
	if s.cpu != nil {
		if v := vcpusFromQuota(s.cpu); v > 0 {
			return v
		}
		if v := vcpusFromCPUSet(s.cpu.Cpus); v > 0 {
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

// cfgMemory is a lock-free snapshot of the fields containerMemory reads.
type cfgMemory struct {
	id       string
	isInfra  bool
	memory   *specs.LinuxMemory
	hugepage []specs.LinuxHugepageLimit
}

func calculateSandboxMemory(ctx context.Context, s *Sandbox) (uint64, error) {
	if s == nil || s.config == nil {
		return 0, fmt.Errorf("sandbox or sandbox config is nil")
	}

	// Snapshot the relevant config fields under RLock, then release before
	// calling activeContainer. See calculateSandboxVCPUs for the rationale.
	s.containersLock.RLock()
	hugePageSupport := s.config.HugePageSupport
	snapshots := make([]cfgMemory, 0, len(s.config.ContainerConfigs))
	for id, cc := range s.config.ContainerConfigs {
		if cc == nil {
			s.containersLock.RUnlock()
			return 0, fmt.Errorf("container config %q is nil", id)
		}
		snap := cfgMemory{id: cc.ID, isInfra: cc.IsInfra}
		if cc.Resources != nil {
			snap.memory = cloneLinuxMemory(cc.Resources.Memory)
			if hugePageSupport && len(cc.Resources.HugepageLimits) > 0 {
				snap.hugepage = append([]specs.LinuxHugepageLimit(nil), cc.Resources.HugepageLimits...)
			}
		}
		snapshots = append(snapshots, snap)
	}
	s.containersLock.RUnlock()

	memorySandbox := uint64(0)
	for _, snap := range snapshots {
		active, err := s.activeContainer(ctx, snap.id)
		if err != nil {
			return 0, err
		}
		if snap.isInfra || !active {
			continue
		}
		memorySandbox += snap.memoryMiB(hugePageSupport)
	}
	return memorySandbox, nil
}

func (s cfgMemory) memoryMiB(hugePageSupport bool) uint64 {
	if s.memory == nil {
		return 0
	}
	var total uint64
	m := s.memory
	if m.Limit != nil && *m.Limit > 0 {
		limitMiB := uint64(*m.Limit >> 20)
		total += limitMiB
		log.Debugf("sandbox memory limit + %d MiB", limitMiB)
	}
	if hugePageSupport {
		for _, lim := range s.hugepage {
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
