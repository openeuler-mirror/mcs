package libmica

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	log "micrun/internal/support/logger"
)

const xlUpdateWorkaroundEnv = "MICRUN_XL_WORKAROUND"

func handleMicaUpdateWithXl(ctx context.Context, h hypervisorControl, id string, opts ...string) error {
	ctx = contextx.OrBackground(ctx)
	if !xlUpdateWorkaroundEnabled() {
		return fmt.Errorf("xl workaround disabled (set %s=1 to enable)", xlUpdateWorkaroundEnv)
	}
	if len(opts) == 0 {
		return fmt.Errorf("update command requires at least one parameter")
	}
	if h == nil || h.Type() != ports.HypervisorXen {
		return fmt.Errorf("xl command workaround only supported on Xen pedestal")
	}

	req, err := parseMicaUpdateRequest(opts)
	if err != nil {
		return err
	}
	value := req.Value
	switch req.Field {
	case MicaUpdateMemoryCurrent:
		memMB, err := parseMicaUint32Resource("memory", value)
		if err != nil {
			return err
		}
		return h.SetMemory(ctx, id, memMB)
	case MicaUpdateMemoryMax:
		memMB, err := parseMicaUint32Resource("max memory", value)
		if err != nil {
			return err
		}
		return h.SetMaxMemory(ctx, id, memMB)
	case MicaUpdateCPUWeight:
		weight, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid CPU weight value %s: %w", value, err)
		}
		if weight < 1 {
			log.Debugf("CPU weight must be >=1, got %d, forcing default 256", weight)
			weight = 256
		}
		return h.SetCPUWeight(ctx, id, uint32(weight))
	case MicaUpdateCPUCapacity:
		capacity, err := parseMicaUint32Resource("CPU capacity", value)
		if err != nil {
			return err
		}
		return h.SetCPUCapacity(ctx, id, capacity)
	case MicaUpdateVCPU:
		vcpuCount, err := parseMicaUint32Resource("VCPU count", value)
		if err != nil {
			return err
		}
		return h.SetVCPUCount(ctx, id, vcpuCount)
	case MicaUpdatePCPUConstraints:
		return fmt.Errorf("PCPU constraint (%s) is not supported by xl fallback", value)
	default:
		return fmt.Errorf("unsupported resource type %s for xl command workaround", req.Field)
	}
}

func parseMicaUint32Resource(name, value string) (uint32, error) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid %s value %s: %w", name, value, err)
	}
	return uint32(parsed), nil
}

func xlUpdateWorkaroundEnabled() bool {
	val := strings.TrimSpace(os.Getenv(xlUpdateWorkaroundEnv))
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
