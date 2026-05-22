package libmica

import (
	"context"
	"fmt"
	"strings"

	ped "micrun/internal/adapters/hypervisor/pedestal"
	"micrun/internal/support/contextx"
	"micrun/internal/support/cpuset"
)

func MaxCPUNum(ctx context.Context) int {
	return int(ped.MaxCPUNum(contextx.OrBackground(ctx)))
}

// isValidCPUString validates the CPU string format
// Supports formats: "1", "1-3", "2-3,15", "1,13,5", ""(empty is All)
// NOTICE: Xen-related validation function
func isValidCPUString(cpuStr string) bool {
	_, err := parseMicaCPUSet(cpuStr)
	return err == nil
}

// ParseCPUString parses the CPU string format and returns individual CPU cores
// Examples: "1-3" -> [1,2,3], "2-3,5" -> [2,3,5], "1,3,5" -> [1,3,5]
func ParseCPUString(cpuStr string) ([]int, error) {
	set, err := parseMicaCPUSet(cpuStr)
	if err != nil {
		return nil, fmt.Errorf("invalid CPU string format: %s", cpuStr)
	}
	return set.ToSlice(), nil
}

func parseMicaCPUSet(cpuStr string) (cpuset.CPUSet, error) {
	// cpuStr == "" means all physical CPUs which are not pinned to Dom0.
	if cpuStr == "" {
		return cpuset.NewCPUSet(), nil
	}
	if strings.TrimSpace(cpuStr) == "" {
		return cpuset.CPUSet{}, fmt.Errorf("empty CPU string")
	}
	return cpuset.Parse(cpuStr)
}

func ClientExists(ctx context.Context, id string) (bool, error) {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return validSocketPath(clientSocketPath(id)), nil
}

func ClientNotExistContext(ctx context.Context, id string) (bool, error) {
	exists, err := ClientExists(ctx, id)
	if err != nil {
		return false, err
	}
	return !exists, nil
}
