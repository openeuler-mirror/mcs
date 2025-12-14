package libmica

import (
	"fmt"
	defs "micrun/definitions"
	ped "micrun/pkg/pedestal"
	"path/filepath"
	"strconv"
	"strings"
)

func MaxCPUNum() int {
	return int(ped.MaxCPUNum())
}

func MaxClientCPUNum() int {
	if defs.IsMock {
		return 1
	}
	return int(ped.ClientCPUCapacity())
}

// validStatusResponse validates if the response string contains valid status information
// This function is kept for backward compatibility, but new code should use parseMicaStatus
// Consider this case: communication with mica daemon failed due to incorrect disconnection
// but status information is received	from mica daemon.
func validStatusResponse(res string) bool {
	if res == "" {
		return false
	}

	// Use the new parsing logic for validation
	status, err := parseMicaStatus(res)
	if err != nil {
		return false
	}

	return status.isValid()
}

// TODO: manually parse statusa
func queryStatus(id string) (string, error) {
	// MicaCtl will construct the path to mica-create.socket
	// Micad will then query the status of each client and generate a comprehensive status output in 'stdout'
	if err := micaCtl(MStatus, id); err != nil {
		return "", fmt.Errorf("failed to query status for client %s via MicaCtl: %w", id, err)
	}
	return "", nil
}

// parseMicaStatus parses the raw status response from micad into MicaStatus struct
// Format: "name                          cpu                state               services"
func parseMicaStatus(rawOutput string) (*MicaStatus, error) {
	if rawOutput == "" {
		return nil, fmt.Errorf("empty response")
	}

	// Check for error responses
	if strings.Contains(rawOutput, defs.MicaFailed) || strings.Contains(rawOutput, "Error") {
		return nil, fmt.Errorf("error response: %s", rawOutput)
	}

	// Parse the formatted response
	// Expected format: "name                          cpu                state               services"
	fields := strings.Fields(rawOutput)
	if len(fields) < 3 {
		return nil, fmt.Errorf("invalid status format: %s", rawOutput)
	}

	// Parse CPU field - now supports multi-core format like
	// "1-3,5" and empty string
	cpuStr := fields[1]
	if !isValidCPUString(cpuStr) {
		return nil, fmt.Errorf("invalid CPU field format: %s", cpuStr)
	}

	// If CPU string is empty, use MaxCPUNum() as fallback
	if cpuStr == "" {
		maxCPU := MaxCPUNum()
		if maxCPU > 0 {
			// Convert to range format (e.g., "0-3" for 4 CPUs)
			cpuStr = fmt.Sprintf("0-%d", maxCPU-1)
		} else {
			return nil, fmt.Errorf("failed to get max CPU number for empty CPU string")
		}
	}

	// Parse state
	state := parseMicaState(fields[2])
	if state == unknown {
		return nil, fmt.Errorf("unknown state: %s", fields[2])
	}

	// Parse services (if any)
	services := parseMicaServices(fields[3:])

	return &MicaStatus{
		Name:     fields[0],
		CPU:      cpuStr,
		State:    state,
		Services: services,
		Raw:      rawOutput,
	}, nil
}

// parseMicaState converts string to MicaState
func parseMicaState(stateStr string) MicaState {
	switch stateStr {
	case "Offline":
		return offline
	case "Configured":
		return configured
	case "Ready":
		return ready
	case "Running":
		return running
	case "Suspended":
		return suspended
	case "Stopped":
		return stopped
	case "Error":
		return stateErr
	// Add more states as needed
	default:
		return unknown
	}
}

// parseMicaServices extracts service information from response fields
func parseMicaServices(fields []string) []MicaService {
	var services []MicaService

	for _, field := range fields {
		serviceStr := strings.ToLower(field)
		switch {
		case strings.Contains(serviceStr, "pty"):
			services = append(services, servicePTY)
		case strings.Contains(serviceStr, "rpc"):
			services = append(services, serviceRPC)
		case strings.Contains(serviceStr, "umt"):
			services = append(services, serviceUMT)
		case strings.Contains(serviceStr, "debug"):
			services = append(services, serviceDebug)
		}
	}

	return services
}

// isValidCPUString validates the CPU string format
// Supports formats: "1", "1-3", "2-3,15", "1,13,5", ""(empty is All)
// NOTICE: Xen-related validation function
func isValidCPUString(cpuStr string) bool {
	// cpuStr == "" means all physical CPUs which are not pinned to Dom0
	if cpuStr == "" {
		return true
	}

	// split by comma for multiple groups
	groups := strings.Split(cpuStr, ",")

	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			return false
		}

		// check if it's a range (contains dash)
		if strings.Contains(group, "-") {
			parts := strings.Split(group, "-")
			if len(parts) != 2 {
				return false
			}

			// validate both parts are integers
			start, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			end, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))

			if err1 != nil || err2 != nil || start < 0 || end < 0 || start > end {
				return false
			}
		} else {
			if _, err := strconv.Atoi(group); err != nil {
				return false
			}
		}
	}

	return true
}

// ParseCPUString parses the CPU string format and returns individual CPU cores
// Examples: "1-3" -> [1,2,3], "2-3,5" -> [2,3,5], "1,3,5" -> [1,3,5]
func ParseCPUString(cpuStr string) ([]int, error) {
	if !isValidCPUString(cpuStr) {
		return nil, fmt.Errorf("invalid CPU string format: %s", cpuStr)
	}

	var cpus []int

	// Empty string means no specific CPUs
	if cpuStr == "" {
		return cpus, nil
	}

	groups := strings.Split(cpuStr, ",")

	for _, group := range groups {
		group = strings.TrimSpace(group)

		if strings.Contains(group, "-") {
			// Range format: "1-3"
			parts := strings.Split(group, "-")
			start, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
			end, _ := strconv.Atoi(strings.TrimSpace(parts[1]))

			for i := start; i <= end; i++ {
				cpus = append(cpus, i)
			}
		} else {
			// Single CPU: "5"
			cpu, _ := strconv.Atoi(group)
			cpus = append(cpus, cpu)
		}
	}

	return cpus, nil
}

func ClientNotExist(id string) bool {
	socketPath := filepath.Join(defs.MicaStateDir, id+".socket")
	valid := validSocketPath(socketPath)
	return !valid
}
