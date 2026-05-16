package container

import (
	"context"
	"fmt"
	"strings"

	"micrun/internal/support/cpuset"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"

	"github.com/hashicorp/go-multierror"
)

func (s *Sandbox) checkVCPUsPinning(ctx context.Context) error {
	if s == nil {
		return er.SandboxNotFound
	}
	if s.config == nil {
		return fmt.Errorf("no sandbox config found")
	}

	if !s.config.EnableVCPUsPinning {
		return nil
	}

	cpus, _, err := s.getSandboxCpusetStr()
	if err != nil {
		return fmt.Errorf("failed to get CPUSet string: %w", err)
	}

	cpuSet, err := cpuset.Parse(cpus)
	if err != nil {
		return fmt.Errorf("failed to parse CPUSet string %s: %w", cpus, err)
	}
	cpuList := cpuSet.ToSlice()

	match := true

	deps, err := s.dependenciesChecked()
	if err != nil {
		return err
	}
	if valid, outOfRangeCPUs := cpusetRangeValidWithLimit(cpuList, deps.HostMaxPhysCPUs(ctx)); !valid {
		match = false
		log.Tracef("these cpus are out of range: %v", outOfRangeCPUs)
	}

	if s.config.SharedCPUPool {
		numVCPUs, numCPUs := int(s.resManager.VCPUCount), len(cpuList)
		if numCPUs != numVCPUs {
			match = false
			log.Tracef("the number of cpusets %d is not equal to the number of vcpus %d", numCPUs, numVCPUs)
		}
	}

	if !match {
		if s.vcpuAlreadyPinned {
			s.vcpuAlreadyPinned = false
			log.Tracef("the sandbox is already pinned to cpusets")
		}
	}

	if err := s.pinVCPU(ctx, cpuSet); err != nil {
		log.Warnf("failed to pin vcpu: %v", err)
		return err
	}

	s.vcpuAlreadyPinned = true
	return nil
}

func (s *Sandbox) getSandboxCpusetStr() (string, string, error) {
	if s == nil {
		return "", "", er.SandboxNotFound
	}
	if s.config == nil {
		return "", "", nil
	}

	cpuResult := cpuset.NewCPUSet()
	memResult := cpuset.NewCPUSet()
	for id, cfg := range s.config.ContainerConfigs {
		if cfg == nil {
			return "", "", fmt.Errorf("container config %q is nil", id)
		}
		if cfg.IsInfra {
			continue
		}
		resource := cfg.Resources
		if resource != nil {
			if resource.CPU == nil {
				continue
			}
			cpuStr := strings.TrimSpace(resource.CPU.Cpus)
			if cpuStr == "" {
				continue
			}
			currCPUSet, err := cpuset.Parse(cpuStr)
			if err != nil {
				return "", "", fmt.Errorf("unable to parse CPUset.cpus for container %s: %w", cfg.ID, err)
			}
			cpuResult = cpuResult.Union(currCPUSet)

			memStr := strings.TrimSpace(resource.CPU.Mems)
			if memStr == "" {
				continue
			}
			currMemSet, err := cpuset.Parse(memStr)
			if err != nil {
				return "", "", fmt.Errorf("unable to parse CPUset.mems for container %s: %w", cfg.ID, err)
			}
			memResult = memResult.Union(currMemSet)
		}
	}

	return cpuResult.String(), memResult.String(), nil
}

func (s *Sandbox) pinVCPU(ctx context.Context, cpuSet cpuset.CPUSet) error {
	if s == nil {
		return er.SandboxNotFound
	}
	if s.config == nil {
		return fmt.Errorf("sandbox config is nil")
	}
	s.resManager.ensureMaps()

	entries, err := s.containerEntries()
	if err != nil {
		return err
	}

	var result *multierror.Error

	if s.config.SharedCPUPool {
		pcpuList := cpuSet.ToSlice()
		for _, entry := range entries {
			cid, c := entry.id, entry.container
			log.Infof("try to pin container %s vcpu affinity to shared cpuset %v", cid, pcpuList)
			if err := c.setVcpuAffinity(ctx, cpuSet); err != nil {
				result = multierror.Append(result, err)
			} else {
				s.resManager.ContainerCPUSet[cid] = cpuSet
			}
		}

		ret := result.ErrorOrNil()
		if ret == nil {
			if total, err := calculateSandboxVCPUs(ctx, s); err == nil {
				s.resManager.VCPUCount = total
			} else {
				s.resManager.VCPUCount = uint32(cpuSet.Size())
			}
		}
		return ret
	}

	allContainerCPUs := cpuset.NewCPUSet()
	for _, entry := range entries {
		cid, c := entry.id, entry.container
		var containerCPUSet cpuset.CPUSet
		if c.config != nil && c.config.Resources != nil && c.config.Resources.CPU != nil && c.config.Resources.CPU.Cpus != "" {
			parsed, err := cpuset.Parse(c.config.Resources.CPU.Cpus)
			if err != nil {
				result = multierror.Append(result, fmt.Errorf("failed to parse cpuset for container %s: %w", cid, err))
				continue
			}
			containerCPUSet = parsed
		} else {
			log.Tracef("container %s has no cpuset specified, skipping CPU pinning", cid)
			continue
		}

		log.Tracef("try to pin container %s vcpu affinity to its own cpuset %v", cid, containerCPUSet.ToSlice())
		if err := c.setVcpuAffinity(ctx, containerCPUSet); err != nil {
			result = multierror.Append(result, err)
		} else {
			s.resManager.ContainerCPUSet[cid] = containerCPUSet
			allContainerCPUs = allContainerCPUs.Union(containerCPUSet)
		}
	}

	ret := result.ErrorOrNil()
	if ret == nil {
		if total, err := calculateSandboxVCPUs(ctx, s); err == nil {
			s.resManager.VCPUCount = total
		} else {
			s.resManager.VCPUCount = uint32(allContainerCPUs.Size())
		}
	}
	return ret
}

func (s *Sandbox) updateResources(ctx context.Context) error {
	if s == nil {
		return er.SandboxNotFound
	}

	if s.config == nil {
		return fmt.Errorf("sandbox config is nil")
	}

	if s.config.InfraOnly {
		return nil
	}

	if s.config.StaticResourceMgmt {
		log.Debug("static resource management is enabled, updating resource is not supported")
		return nil
	}

	sandboxVCPUs, err := calculateSandboxVCPUs(ctx, s)
	if err != nil {
		return err
	}

	sandboxVCPUs += s.config.PedConfig.MiniVCPUNum

	newSandboxMemoryMB, err := calculateSandboxMemory(ctx, s)
	if err != nil {
		return err
	}

	oldVCPUs, newVCPUs := s.resManager.resizeVCPUs(sandboxVCPUs)
	if oldVCPUs != newVCPUs {
		log.Infof("sandbox total vcpu number from %d to %d", oldVCPUs, newVCPUs)
	}

	oldMemBytes, newMemBytes := s.resManager.resizeMemory(newSandboxMemoryMB)
	if oldMemBytes != newMemBytes {
		log.Infof("sandbox total memory usage from %d MiB to %d MiB", oldMemBytes>>20, newMemBytes>>20)
	}

	return nil
}
