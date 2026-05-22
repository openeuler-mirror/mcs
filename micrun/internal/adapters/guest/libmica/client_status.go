package libmica

import (
	"context"
	"fmt"
	"strings"

	"micrun/internal/support/contextx"
	defs "micrun/internal/support/definitions"
)

// MicaStatus represents the complete status of a MICA client.
type MicaStatus struct {
	Name     string        `json:"name"`
	CPU      string        `json:"cpu"`
	State    MicaState     `json:"state"`
	Services []MicaService `json:"services"`
	Raw      string        `json:"raw"`
}

type micaStatusFields struct {
	name     string
	cpu      string
	state    string
	services []string
}

type maxCPUProvider func(context.Context) int

func (ms MicaStatus) String() string {
	return fmt.Sprintf("Name: %s, CPU: %s, State: %s, Services: %v",
		ms.Name, ms.CPU, ms.State, ms.Services)
}

func (ms MicaStatus) IsStopped() bool {
	return ms.State == stopped
}

func (ms MicaStatus) isValid() bool {
	return ms.Name != "" && isValidCPUString(ms.CPU) && ms.State != unknown
}

// Status returns structured status information for a specific client.
//
// Deprecated: use StatusContext or StatusWithHypervisor so callers can
// propagate cancellation and host CPU policy explicitly.
func Status(id string) (*MicaStatus, error) {
	return StatusContext(context.Background(), id)
}

func StatusContext(ctx context.Context, id string) (*MicaStatus, error) {
	return statusWithCPUProvider(ctx, id, defaultMaxCPUProvider)
}

func StatusWithHypervisor(ctx context.Context, id string, h hypervisorControl) (*MicaStatus, error) {
	maxCPUs := defaultMaxCPUProvider
	if h != nil {
		maxCPUs = func(ctx context.Context) int {
			return int(h.MaxCPUNum(ctx))
		}
	}
	return statusWithCPUProvider(ctx, id, maxCPUs)
}

func statusWithCPUProvider(ctx context.Context, id string, maxCPUs maxCPUProvider) (*MicaStatus, error) {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	res, err := queryStatus(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get status for client %s: %w", id, err)
	}

	status, err := parseMicaStatusWithCPUProvider(ctx, res, maxCPUs)
	if err != nil {
		return nil, fmt.Errorf("failed to parse status for client %s: %w", id, err)
	}

	if !status.isValid() {
		return nil, fmt.Errorf("invalid status for client %s: %s", id, status.Raw)
	}

	return status, nil
}

func defaultMaxCPUProvider(ctx context.Context) int {
	return MaxCPUNum(ctx)
}

func queryStatus(ctx context.Context, id string) (string, error) {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := ensureMicadListening(); err != nil {
		return "", err
	}

	if err := validateClientID(id); err != nil {
		return "", err
	}

	missing, err := ClientNotExistContext(ctx, id)
	if err != nil {
		return "", err
	}
	if missing {
		return "", fmt.Errorf("client %s does not exist", id)
	}

	s := newMicaSocket(clientSocketPath(id))
	res, err := s.handleMsgWithResponse(ctx, []byte(string(MStatus)))
	if err != nil {
		return "", fmt.Errorf("failed to query status for client %s via client control socket: %w", id, err)
	}
	return res, nil
}

// parseMicaStatus parses the raw status response from micad into MicaStatus struct.
// Format: "name                          cpu                state               services"
//
// Deprecated: use parseMicaStatusWithCPUProvider when parsing live micad output
// because an empty CPU field needs host CPU policy to expand to the full range.
func parseMicaStatus(rawOutput string) (*MicaStatus, error) {
	return parseMicaStatusWithCPUProvider(context.Background(), rawOutput, nil)
}

func parseMicaStatusWithCPUProvider(ctx context.Context, rawOutput string, maxCPUs maxCPUProvider) (*MicaStatus, error) {
	fields, err := splitMicaStatusFields(rawOutput)
	if err != nil {
		return nil, err
	}

	cpuStr, err := normalizeMicaCPUField(ctx, fields.cpu, maxCPUs)
	if err != nil {
		return nil, err
	}

	state := parseMicaState(fields.state)
	if state == unknown {
		return nil, fmt.Errorf("unknown state: %s", fields.state)
	}

	services := parseMicaServices(fields.services)

	return &MicaStatus{
		Name:     fields.name,
		CPU:      cpuStr,
		State:    state,
		Services: services,
		Raw:      rawOutput,
	}, nil
}

func splitMicaStatusFields(rawOutput string) (micaStatusFields, error) {
	if rawOutput == "" {
		return micaStatusFields{}, fmt.Errorf("empty response")
	}

	if strings.Contains(rawOutput, defs.MicaFailed) {
		return micaStatusFields{}, fmt.Errorf("error response: %s", rawOutput)
	}

	fields := strings.Fields(rawOutput)
	if len(fields) < 2 {
		return micaStatusFields{}, fmt.Errorf("invalid status format: %s", rawOutput)
	}
	if parseMicaState(fields[1]) != unknown {
		return micaStatusFields{
			name:     fields[0],
			cpu:      "",
			state:    fields[1],
			services: fields[2:],
		}, nil
	}
	if len(fields) < 3 {
		return micaStatusFields{}, fmt.Errorf("invalid status format: %s", rawOutput)
	}

	return micaStatusFields{
		name:     fields[0],
		cpu:      fields[1],
		state:    fields[2],
		services: fields[3:],
	}, nil
}

func normalizeMicaCPUField(ctx context.Context, cpuStr string, maxCPUs maxCPUProvider) (string, error) {
	if cpuStr == "" {
		if maxCPUs == nil {
			return "", fmt.Errorf("failed to get max CPU number for empty CPU string")
		}
		maxCPU := maxCPUs(contextx.OrBackground(ctx))
		if maxCPU > 0 {
			cpuStr = fmt.Sprintf("0-%d", maxCPU-1)
		} else {
			return "", fmt.Errorf("failed to get max CPU number for empty CPU string")
		}
	}

	set, err := parseMicaCPUSet(cpuStr)
	if err != nil {
		return "", fmt.Errorf("invalid CPU field format: %s", cpuStr)
	}
	return set.String(), nil
}

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
	default:
		return unknown
	}
}

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
