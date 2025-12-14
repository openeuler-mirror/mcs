package micantainer

import (
	"context"
	"errors"
	"fmt"
	er "micrun/errors"
	log "micrun/logger"
	"micrun/pkg/cpuset"
	"micrun/pkg/libmica"
	"micrun/pkg/pedestal"
	"micrun/pkg/utils"
	"os/exec"
	"strings"
	"time"
)

func startClient(ctx context.Context, sandbox SandboxTraits, c *Container) error {
	if _, err := c.ensureClientPresence(); err != nil {
		return err
	}

	start := time.Now()
	if err := libmica.Start(c.id); err != nil {
		log.Errorf("startClient: Start failed: %v", err)
		return err
	}

	if err := c.setupMemory(); err != nil {
		return err
	}
	log.Infof("startClient: Start OK in %s", time.Since(start))

	return nil
}

// 1. search in bundle/.../<clientOSname>.elf
// 2. if missing, log and search for binary in bundle recursively
// TODO: Only copy values, the evaluation procedure is in the caller function
func createMicaClientConf(container *Container) (libmica.MicaClientConf, error) {
	config := container.config
	pedType := HostPedType
	cpus := container.GetClientCPU()
	conf := libmica.MicaClientConf{}
	cpuCap := int(config.cpuCapacity())
	// Pre-calculate effective values for clarity.
	// Use VCPUNum prepared in ContainerConfig; it already reflects cpuset policy
	// or defaults to 1 when not specified.
	vcpus := int(config.VCPUNum)
	if vcpus <= 0 {
		vcpus = 1
	}
	// memoryMB (initial) should prefer the configured limit, falling back to the minimum (reservation) when unset.
	memMB := int(config.containerMaxMemMB())
	if err := ensureFirmwarePath(config.ImageAbsPath); err != nil {
		return libmica.MicaClientConf{}, fmt.Errorf("firmware validation failed: %w", err)
	}

	// Memory limit is already expressed in MiB
	conf.InitWithOpts(libmica.MicaClientConfCreateOptions{
		CPU:             cpus,
		CPUCapacity:     cpuCap,
		CPUWeight:       int(pedestal.ShareToWeight(config.cpuShares())),
		VCPUs:           vcpus,
		MaxVCPUs:        int(config.MaxVcpuNum),
		MemoryMB:        memMB,
		MemoryThreshold: int(config.MemoryThresholdMB),
		Name:            container.id,
		Path:            config.ImageAbsPath,
		Ped:             pedType.String(),
		PedCfg:          config.PedestalConf,
	})
	return conf, nil
}

// pCPU = physical CPU 实际的CPU （核数）, 未来需要考虑更复杂的异构场景，但这里先不考虑，只要确保话别说死就行
// - 如果有一个容器设置了cpuset(AFFINITY)，对于该容器而言，调度器不会再允许它运行在cpuset之外的pcpu上了
// - 如果一个sandbox中有多个容器都设置了cpuset，我们可以考虑它们的cpuset并集为一个 cpu pool, (shared_cpu_pool option)
//
// 如果启用了 shared_cpu_pool:
// sandbox 内的 所有容器都只能运行在这个 cpu pool 的pcpu上。
// 目前这是一个仅在 MicRan 中保留的概念，未来**或许**我们会完成对pedestal
// cpu pool 的实际操控, 那么sandbox 为容器workload 申请的 vcpu number = Size(cpuSetUnion)
// 并且这个 sandbox 管理 cpupool 的点子未必合适，因为机器上可能有多个sandbox, sandbox 管理的 cpuppol 的范围有可能重合，比如
// sandbox1: (0,1,2,) sandbox2: (1,2,3) 这种情况
//
// 看代码的人，你可能会有疑问：为什么要考虑VCPU的数量呢？hypervisor的affinity明明只是限定 pcpu set到若干vcpu上，从性能上看vcpu肯定是越小越好的
// 是因为这或许可以 **反映出** RTOS 内部能看到的 VCPU 的数量和实际分给它的 PCPU 数量的对应关系，这是一个**面向PPT**的设计(vcpu_pcpu_binding option)
// 最好的做法应该是默认VCPU = 1，必须要在 runtime config 或者 annotation 中打开某个开关（还没加) 才启用 VCPU = Size(cpuSetUnion) 也就是 Num of pCPUs
// 那么VCPU和PCPU数量的对应关系是这样的：
// 1. 启用了某种 面向PPT(vcpu_pcpu_binding) 设计： VCPUs :PCPUs = 1:1
// 2. 通常情况下
// > for sandbox: VCPUs : PCPUs = 1:N, N = Size(cpuSetUnion) or = Sum(cpuCapacity),
// > for container: VCPUs : PCPUs = 1:M, M = Size(cpuSet) or = cpuCapacity
//
// 在算力上，应该设置capcapacity为=0,使pedestal(hypervisor)不限制cpu用量
// calculateSandboxVCPUs returns the total VCPU count for the sandbox.
// Without a resource pool, this is a statistic that should reflect the sum
// of each container's configured vCPUs in the sandbox.
func calculateSandboxVCPUs(s *Sandbox) (uint32, error) {
	if s == nil || s.config == nil {
		return 0, fmt.Errorf("sandbox or sandbox config is nil")
	}

	total := uint32(0)
	for _, cc := range s.config.ContainerConfigs {
		if cc.IsInfra {
			continue
		}
		if c, ok := s.containers[cc.ID]; ok {
			state := c.checkState()
			if state == StateStopped || state == StateDown {
				log.Debugf("skipped inactive container %s (state=%s)", c.ID(), state)
				continue
			}
		}

		if cc.VCPUNum > 0 {
			total += cc.VCPUNum
			continue
		}

		if cc.Resources != nil && cc.Resources.CPU != nil {
			cpu := cc.Resources.CPU
			if cpu.Period != nil && cpu.Quota != nil && *cpu.Period != 0 {
				m := utils.CalculateMilliCPUs(*cpu.Quota, *cpu.Period)
				v := utils.CalculateVCpusFromMilliCpus(m)
				if v > 0 {
					total += v
					continue
				}
			}
			if cpu.Cpus != "" {
				set, err := cpuset.Parse(cpu.Cpus)
				if err == nil {
					total += uint32(set.Size())
					continue
				}
			}
		}

		// Last resort: count 1.
		total += 1
	}

	return total, nil
}

func calculateSandboxMemory(s *Sandbox) uint64 {
	// Return value is in MiB
	memorySandbox := uint64(0)
	for _, cc := range s.config.ContainerConfigs {
		if cc.IsInfra {
			continue
		}
		if c, ok := s.containers[cc.ID]; ok {
			state := c.checkState()
			if state == StateStopped || state == StateDown {
				log.Debugf("skipped inactive container %s (state=%s)", c.ID(), state)
				continue
			}
		}

		if cc.Resources == nil {
			continue
		}

		if m := cc.Resources.Memory; m != nil {
			// OCI memory limit is in bytes; convert to MiB for sandbox accounting
			if m.Limit != nil && *m.Limit > 0 {
				limitMiB := uint64(*m.Limit >> 20)
				memorySandbox += limitMiB
				log.Debugf("sandbox memory limit + %d MiB", limitMiB)
			}

			// Hugepage limits are also in bytes; convert to MiB
			if s.config.HugePageSupport {
				for _, lim := range cc.Resources.HugepageLimits {
					hpMiB := lim.Limit >> 20
					log.Debugf("sandbox hugepage limit + %d MiB (%s)", hpMiB, lim.Pagesize)
					memorySandbox += hpMiB
				}
			}
		}
	}
	return memorySandbox
}

func CpusetRangeValid(sortedCpuList []int) (bool, []int) {
	maxCpus := pedestal.HostCPUCounts().Physical
	outrange := []int{}

	for _, cpu := range sortedCpuList {
		// cpuid start from 0
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

// Update resource for changed resource
func updateContainerResource(c *Container, updated *pedestal.EssentialResource) error {
	if c == nil {
		return fmt.Errorf("missing container reference when updating resources")
	}
	exec := &c.me
	old := exec.ReadResource()

	log.Debugf("Resource update for container %s: old=%s, new=%s",
		c.id, formatResourceForLog(old), formatResourceForLog(updated))

	// Nil-safety checks for all pointer fields
	if updated.CpuCpacity != nil {
		if exec.NeedUpdateCpuCap(*updated.CpuCpacity) {
			err := exec.UpdateCPUCapacity(*updated.CpuCpacity)
			if err != nil {
				return fmt.Errorf("failed to update cpu capacity of %s: %v", c.id, err)
			}
			if *updated.CpuCpacity == 0 {
				log.Infof("container %s's cpu capacity is unlimited", c.id)
			}
		}
	}

	if updated.MemoryMaxMB != nil {
		if exec.NeedUpdateMemLimit(*updated.MemoryMaxMB) {
			err := exec.EnsureMemoryLimit(*updated.MemoryMaxMB)
			if err != nil {
				return fmt.Errorf("failed to update max memory of %s: %v", c.id, err)
			}
		}
	}

	if exec.NeedUpdateCpuSet(old.ClientCpuSet, updated.ClientCpuSet) {
		err := exec.UpdatePCPUConstrains(updated.ClientCpuSet)
		if err != nil {
			return fmt.Errorf("failed to update cpuset of vcpu: %v", err)
		}
	}

	if updated.CPUWeight != nil {
		if exec.NeedUpdateCpuShare(*updated.CPUWeight) {
			err := exec.UpdateCPUWeight(*updated.CPUWeight)
			if err != nil {
				return fmt.Errorf("failed to set a different cpu weight for %s: %v", c.id, err)
			}
		}
	}

	if old.Vcpu != nil && updated.Vcpu != nil {
		if exec.NeedUpdateVCpus(*updated.Vcpu) {
			old, newer, err := exec.UpdateVCPUNum(*updated.Vcpu)
			if err != nil {
				log.Warnf("failed to update vcpu number: %v", err)
			}
			if old != newer {
				log.Infof("update vcpu number from %d to %d", old, newer)
			}
		}
	}

	return nil
}

// formatResourceForLog formats EssentialResource for readable logging
func formatResourceForLog(res *pedestal.EssentialResource) string {
	if res == nil {
		return "<nil>"
	}

	var parts []string

	if res.CpuCpacity != nil {
		parts = append(parts, fmt.Sprintf("CpuCapacity=%d", *res.CpuCpacity))
	}

	if res.CPUWeight != nil {
		parts = append(parts, fmt.Sprintf("CPUWeight=%d", *res.CPUWeight))
	}

	if res.ClientCpuSet != "" {
		parts = append(parts, fmt.Sprintf("ClientCpuSet=%s", res.ClientCpuSet))
	}

	if res.Vcpu != nil {
		parts = append(parts, fmt.Sprintf("Vcpu=%d", *res.Vcpu))
	}

	if res.MemoryMaxMB != nil {
		parts = append(parts, fmt.Sprintf("MemoryLimitMB=%d", *res.MemoryMaxMB))
	}

	if len(parts) == 0 {
		return "<empty>"
	}

	return "{" + strings.Join(parts, ", ") + "}"
}

func ensureFirmwarePath(firmwarePath string) error {

	absPath, err := utils.EnsureRegularFilePath(firmwarePath)
	if err != nil {
		return err
	}

	log.Debugf("firmware path validated: %s", absPath)
	return nil
}

func copyUint32(v uint32) *uint32 {
	val := v
	return &val
}

// loadSandbox restores a sandbox from disk by its ID.
func loadSandbox(ctx context.Context, id string) (sandbox *Sandbox, err error) {
	if id == "" {
		return nil, er.EmptySandboxID
	}

	ss, err := restoreSandbox(ctx, id)
	if err != nil {
		log.Debugf("Failed to restore sandbox from disk: %v.", err)
		return nil, err
	}
	c := ss.Config

	sandbox, err = createSandbox(ctx, &c)
	if err != nil {
		log.Errorf("Failed to create sandbox: %v.", err)
		return nil, err
	}

	if err := sandbox.loadContainersToSandbox(ctx); err != nil {
		return nil, err
	}
	return sandbox, nil
}

func extractENo(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 255
}

func copyInt64(src *int64) *int64 {
	if src == nil {
		return nil
	}
	val := *src
	return &val
}

func copyUint64(src *uint64) *uint64 {
	if src == nil {
		return nil
	}
	val := *src
	return &val
}

func copyBool(src *bool) *bool {
	if src == nil {
		return nil
	}
	val := *src
	return &val
}
