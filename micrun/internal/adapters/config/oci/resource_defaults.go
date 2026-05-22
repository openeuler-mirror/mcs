package oci

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	cntr "micrun/internal/domain/container"
	ann "micrun/internal/support/annotations"
	defs "micrun/internal/support/definitions"
	log "micrun/internal/support/logger"
)

// formatCPULimit formats CPU limit information into human readable string.
func formatCPULimit(config *cntr.ContainerConfig) string {
	if config == nil {
		return "unlimited"
	}

	var parts []string
	if limit := config.CPUCapacity(); limit > 0 {
		parts = append(parts, fmt.Sprintf("limit=%d cores", limit))
	}

	cpu := config.Resources
	if cpu != nil && cpu.CPU != nil {
		if cpu.CPU.Quota != nil && cpu.CPU.Period != nil && *cpu.CPU.Period != 0 && *cpu.CPU.Quota > 0 {
			ratio := float64(*cpu.CPU.Quota) / float64(*cpu.CPU.Period)
			parts = append(parts, fmt.Sprintf("quota=%.2f cores", ratio))
		}
		if shares := config.CPUShares(); shares > 0 {
			parts = append(parts, fmt.Sprintf("shares=%d", shares))
		}
		if cpuset := config.CPUSet(); cpuset != "" {
			parts = append(parts, fmt.Sprintf("cpuset=%s", cpuset))
		}
	}

	if len(parts) == 0 {
		return "unlimited"
	}
	return strings.Join(parts, ", ")
}

// formatMemoryLimit formats memory limit information into human readable string.
func formatMemoryLimit(config *cntr.ContainerConfig) string {
	if config == nil {
		return "unlimited"
	}

	var parts []string
	if limit := config.MemoryLimitMiB(); limit > 0 {
		parts = append(parts, fmt.Sprintf("limit=%s", formatBytes(int64(limit)*1024*1024)))
	}
	if reservation := config.MemoryReservationMiB(); reservation > 0 {
		parts = append(parts, fmt.Sprintf("reservation=%s", formatBytes(int64(reservation)*1024*1024)))
	}
	if len(parts) == 0 {
		return "unlimited"
	}
	return strings.Join(parts, ", ")
}

func applyContainerRuntimeDefaults(config *cntr.ContainerConfig, annotations map[string]string, runtimeConfig *RuntimeConfig) error {
	if config == nil {
		return nil
	}
	if runtimeConfig == nil {
		return fmt.Errorf("runtime config is required")
	}

	if config.MemoryReservationMiB() == 0 {
		if runtimeConfig.MinContainerMemMB > 0 {
			config.SetMemoryReservationMB(runtimeConfig.MinContainerMemMB)
		} else {
			config.SetMemoryReservationMB(defs.DefaultMinMemMB)
		}
	}

	if limit := config.MemoryLimitMiB(); limit > 0 {
		if reservation := config.MemoryReservationMiB(); reservation > limit {
			config.SetMemoryReservationMB(limit)
		}
	}

	config.MaxVcpuNum = resolveMaxVcpu(annotations, runtimeConfig)
	config.MemoryThresholdMB = calculateClientMemThreshold(config, runtimeConfig)
	return nil
}

func resolveMaxVcpu(annotations map[string]string, runtimeCfg *RuntimeConfig) uint32 {
	if annotations != nil {
		if value, ok := annotations[ann.ContainerMaxVcpuNum]; ok {
			value = strings.TrimSpace(value)
			if value == "" {
				return runtimeMaxVcpuOrDefault(runtimeCfg)
			}
			if parsed, err := strconv.ParseUint(value, 10, 32); err == nil && parsed > 0 {
				return uint32(parsed)
			} else if err != nil {
				log.Debugf("invalid %s %q: %v", ann.ContainerMaxVcpuNum, value, err)
			}
		}
	}

	return runtimeMaxVcpuOrDefault(runtimeCfg)
}

func runtimeMaxVcpuOrDefault(runtimeCfg *RuntimeConfig) uint32 {
	if runtimeCfg != nil && runtimeCfg.MaxContainerVCPUs > 0 {
		return runtimeCfg.MaxContainerVCPUs
	}
	return defs.DefaultMaxVCPUs
}

// calculateClientMemThreshold calculates the memory threshold for RTOS client.
func calculateClientMemThreshold(config *cntr.ContainerConfig, runtimeCfg *RuntimeConfig) uint32 {
	maxMem := config.MemoryLimitMiB()
	if maxMem == 0 {
		maxMem = config.MemoryReservationMiB()
	}
	if maxMem == 0 && runtimeCfg != nil && runtimeCfg.MinContainerMemMB > 0 {
		maxMem = runtimeCfg.MinContainerMemMB
	}
	if maxMem == 0 {
		maxMem = defs.DefaultMinMemMB
	}
	if maxMem > math.MaxUint32/2 {
		return math.MaxUint32 - 1
	}

	doubled := maxMem * 2
	if doubled == 0 {
		doubled = defs.DefaultMinMemMB * 2
	}
	return doubled
}

// formatBytes formats bytes into human readable string.
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
