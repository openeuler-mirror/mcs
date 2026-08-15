package container

import (
	"context"
	"fmt"
	"strings"

	"micrun/internal/support/cpuset"
	er "micrun/internal/support/errors"
	"micrun/internal/support/lockutil"
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
		// In shared-pool mode, currentVCPUCount() returns the SUM of all
		// active containers' VCPUNum (= N * poolSize), while len(cpuList)
		// is the pool size. The comparison is only meaningful with a single
		// active container; otherwise N*poolSize != poolSize and the check
		// always reports a spurious mismatch (harmless — pinVCPU runs
		// unconditionally — but floods the trace log).
		numVCPUs, numCPUs := int(s.currentVCPUCount()), len(cpuList)
		if numVCPUs > 0 && numCPUs > numVCPUs {
			// Only flag a real under-provisioning: pool size exceeds the
			// sandbox total, meaning at least one container is under-pinned.
			match = false
			log.Tracef("the number of cpusets %d exceeds the total vcpus %d", numCPUs, numVCPUs)
		}
	}

	if !match {
		log.Tracef("cpuset does not match current vcpu configuration, will re-pin")
	}

	if err := s.pinVCPU(ctx, cpuSet); err != nil {
		log.Warnf("failed to pin vcpu: %v", err)
		return err
	}

	return nil
}

func (s *Sandbox) getSandboxCpusetStr() (string, string, error) {
	if s == nil {
		return "", "", er.SandboxNotFound
	}
	if s.config == nil {
		return "", "", nil
	}

	s.containersLock.RLock()
	defer s.containersLock.RUnlock()
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
	lockutil.WithLock(&s.resMu, func() { s.resManager.ensureMaps() })

	entries, err := s.containerEntries()
	if err != nil {
		return err
	}

	var result *multierror.Error
	// pinned collects successful (containerID -> cpuset) pairs; we apply them
	// to resManager under resMu after the potentially-blocking guest calls.
	pinned := make(map[string]cpuset.CPUSet)

	if s.config.SharedCPUPool {
		// If no container specifies a CPU set, there is no shared pool to
		// pin to. Skip pinning rather than calling VCPUPin with an empty
		// list, which the guest executor rejects.
		if cpuSet.Size() == 0 {
			return nil
		}
		pcpuList := cpuSet.ToSlice()
		for _, entry := range entries {
			cid, c := entry.id, entry.container
			pinnable, pinErr := s.pinnableContainer(ctx, c)
			if pinErr != nil {
				result = multierror.Append(result, pinErr)
				continue
			}
			if !pinnable {
				continue
			}
			log.Infof("try to pin container %s vcpu affinity to shared cpuset %v", cid, pcpuList)
			if err := c.setVcpuAffinity(ctx, cpuSet); err != nil {
				result = multierror.Append(result, err)
			} else {
				pinned[cid] = cpuSet
			}
		}

		ret := result.ErrorOrNil()
		if ret == nil {
			var total uint32
			if calculated, err := calculateSandboxVCPUs(ctx, s); err == nil {
				total = calculated
			} else {
				total = uint32(cpuSet.Size())
			}
			s.applyPinnedCPUSet(pinned, total)
		}
		return ret
	}

	allContainerCPUs := cpuset.NewCPUSet()
	for _, entry := range entries {
		cid, c := entry.id, entry.container
		// Snapshot the cpuset string under containersLock so a concurrent
		// UpdateContainer/setVcpuAffinity cannot tear the string read (Cpus
		// is a non-atomic pointer+length pair).
		cpuStr := ""
		s.containersLock.RLock()
		if c.config != nil && c.config.Resources != nil && c.config.Resources.CPU != nil {
			cpuStr = c.config.Resources.CPU.Cpus
		}
		s.containersLock.RUnlock()
		cpuStr = strings.TrimSpace(cpuStr)
		var containerCPUSet cpuset.CPUSet
		if cpuStr != "" {
			parsed, err := cpuset.Parse(cpuStr)
			if err != nil {
				result = multierror.Append(result, fmt.Errorf("failed to parse cpuset for container %s: %w", cid, err))
				continue
			}
			containerCPUSet = parsed
		} else {
			log.Tracef("container %s has no cpuset specified, skipping CPU pinning", cid)
			continue
		}

		pinnable, pinErr := s.pinnableContainer(ctx, c)
		if pinErr != nil {
			result = multierror.Append(result, pinErr)
			continue
		}
		if !pinnable {
			continue
		}

		log.Tracef("try to pin container %s vcpu affinity to its own cpuset %v", cid, containerCPUSet.ToSlice())
		if err := c.setVcpuAffinity(ctx, containerCPUSet); err != nil {
			result = multierror.Append(result, err)
		} else {
			pinned[cid] = containerCPUSet
			allContainerCPUs = allContainerCPUs.Union(containerCPUSet)
		}
	}

	ret := result.ErrorOrNil()
	if ret == nil {
		var total uint32
		if calculated, err := calculateSandboxVCPUs(ctx, s); err == nil {
			total = calculated
		} else {
			total = uint32(allContainerCPUs.Size())
		}
		s.applyPinnedCPUSet(pinned, total)
	}
	return ret
}

// pinnableContainer reports whether a container should receive a vCPU
// affinity pin. Infra (pause) containers never register a mica client, so
// pinning one always fails on the missing control socket; stopped/down
// containers have no live domain, so micad's underlying xl vcpu-pin always
// fails. Either failure would poison the aggregated pin result and fail the
// triggering sibling's Create/Start/Update — kubelet's standard container
// restart leaves the old stopped container registered until GC deletes it,
// so the very next CreateContainer in the pod would fail on the dead
// sibling. Mirrors the infra filter in getSandboxCpusetStr and the
// activeContainer filter in calculateSandboxVCPUs. A nil container is
// reported pinnable so setVcpuAffinity keeps returning ContainerNotFound.
func (s *Sandbox) pinnableContainer(ctx context.Context, c *Container) (bool, error) {
	if c == nil {
		return true, nil
	}
	if c.isInfra() {
		return false, nil
	}
	return s.activeContainer(ctx, c.ID())
}

// applyPinnedCPUSet writes the successfully-pinned container→cpuset pairs
// into resManager under resMu, skipping any container that was deleted
// while the blocking Xen affinity calls were in flight. Without this
// re-check, a concurrent DeleteContainer would leave a stale entry in
// ContainerCPUSet that never gets cleaned up.
func (s *Sandbox) applyPinnedCPUSet(pinned map[string]cpuset.CPUSet, total uint32) {
	// Check container existence under containersLock first (matching the
	// documented lock order: containersLock → resMu), then apply under resMu.
	s.containersLock.RLock()
	live := make(map[string]cpuset.CPUSet, len(pinned))
	for cid, set := range pinned {
		if _, ok := s.containers[cid]; ok {
			live[cid] = set
		}
	}
	s.containersLock.RUnlock()

	lockutil.WithLock(&s.resMu, func() {
		for cid, set := range live {
			s.resManager.ContainerCPUSet[cid] = set
		}
		s.resManager.VCPUCount = total
	})
}

func (s *Sandbox) updateResources(ctx context.Context) error {
	if s == nil {
		return er.SandboxNotFound
	}

	if s.config == nil {
		return fmt.Errorf("sandbox config is nil")
	}

	// Read InfraOnly under containersLock: CreateContainer flips it (with
	// the lock held) once a non-infra container joins the sandbox; an
	// unsynchronized read races with that write and can make this function
	// skip resource accounting with stale state.
	infraOnly := lockutil.WithReadLockValue(&s.containersLock, func() bool {
		return s.config.InfraOnly
	})
	if infraOnly {
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

	var oldVCPUs, newVCPUs uint32
	var oldMemBytes, newMemBytes uint64
	lockutil.WithLock(&s.resMu, func() {
		oldVCPUs, newVCPUs = s.resManager.resizeVCPUs(sandboxVCPUs)
		oldMemBytes, newMemBytes = s.resManager.resizeMemory(newSandboxMemoryMB)
	})
	if oldVCPUs != newVCPUs {
		log.Infof("sandbox total vcpu number from %d to %d", oldVCPUs, newVCPUs)
	}
	if oldMemBytes != newMemBytes {
		log.Infof("sandbox total memory usage from %d MiB to %d MiB", oldMemBytes>>20, newMemBytes>>20)
	}

	return nil
}

// currentVCPUCount returns the sandbox-wide VCPU count under resMu.
func (s *Sandbox) currentVCPUCount() uint32 {
	return lockutil.WithLockValue(&s.resMu, func() uint32 {
		return s.resManager.VCPUCount
	})
}
