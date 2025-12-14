package libmica

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	log "micrun/logger"
	"micrun/pkg/pedestal"
)

func handleMicaUpdateWithXl(id string, opts ...string) error {
	if !xlAsMicaUpdate() {
		return fmt.Errorf("xl workaround disabled (set MICRUN_XL_WORKAROUND=1 to enable)")
	}
	if len(opts) == 0 {
		return fmt.Errorf("update command requires at least one parameter")
	}
	if pedestal.GetHostPed() != pedestal.Xen {
		return fmt.Errorf("xl command workaround only supported on Xen pedestal")
	}

	resourceType, value := parseUpdateArgs(opts)
	if resourceType == "" {
		return fmt.Errorf("invalid update parameters: %v", opts)
	}
	switch resourceType {
	case "Memory":
		memMB, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid memory value %s: %w", value, err)
		}
		return pedestal.XlMemSet(id, memMB)
	case "MaxMem":
		memMB, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid max memory value %s: %w", value, err)
		}
		return pedestal.XlMemMax(id, memMB)
	case "CPUWeight":
		weight, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid CPU weight value %s: %w", value, err)
		}
		if weight < 1 {
			log.Debugf("CPU weight must be >=1, got %d, forcing default 256", weight)
			weight = 256
		}
		return pedestal.XlSchedCredit2(id, weight, 0)
	case "CPUCpacity":
		capacity, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid CPU capacity value %s: %w", value, err)
		}
		return pedestal.XlSchedCredit2(id, 0, capacity)
	case "VCPU":
		vcpuCount, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid VCPU count value %s: %w", value, err)
		}
		return pedestal.XlVcpuSet(id, vcpuCount)
	case "CPU":
		log.Infof("PCPU constraint (%s) not implemented in xl fallback, skipping", value)
		return nil
	default:
		return fmt.Errorf("unsupported resource type %s for xl command workaround", resourceType)
	}
}

// xlAsMicaUpdate is a workaround for mica set command
func xlAsMicaUpdate() bool {
	val := strings.TrimSpace(os.Getenv("MICRUN_XL_WORKAROUND"))
	if val == "" {
		return false
	}
	switch strings.ToLower(val) {
	case "0", "false", "no":
		return false
	default:
		return true
	}
}

func parseUpdateArgs(opts []string) (resourceType, value string) {
	if len(opts) == 1 {
		parts := strings.Fields(opts[0])
		if len(parts) == 0 {
			return "", ""
		}
		resourceType = parts[0]
		if len(parts) > 1 {
			value = strings.Join(parts[1:], " ")
		}
		return resourceType, value
	}
	resourceType = opts[0]
	if len(opts) > 1 {
		value = strings.Join(opts[1:], " ")
	}
	return resourceType, value
}
