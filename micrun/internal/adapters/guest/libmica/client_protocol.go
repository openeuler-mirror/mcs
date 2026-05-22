// Package libmica provides client functionality for interacting with the MICA daemon.
package libmica

import (
	"context"

	"micrun/internal/ports"
)

type MicaCommand string
type MicaUpdateField string
type PedType int
type MicaState string
type MicaService string

const (
	MCreate MicaCommand = "create"
	MStart  MicaCommand = "start"
	MStop   MicaCommand = "stop"
	MRemove MicaCommand = "rm"
	MPause  MicaCommand = "pause"
	MResume MicaCommand = "resume"
	MStatus MicaCommand = "status"
	// mica set <short_id> MemoryInMiB/CPUCapacity <value>
	MUpdate MicaCommand = "set"

	MaxNameLen         = 66
	MaxPedLen          = 16
	MaxFirmwarePathLen = 256
	MaxCPUStringLen    = 128
	MaxConfigStrLen    = 512
)

const (
	Xen PedType = iota
	Baremetal
)

const (
	unknown    MicaState = "unknown"
	offline    MicaState = "Offline"
	configured MicaState = "Configured"
	ready      MicaState = "Ready"
	running    MicaState = "Running"
	suspended  MicaState = "Suspended"
	stopped    MicaState = "Stopped"
	stateErr   MicaState = "Error"
)

const (
	servicePTY   MicaService = "pty"
	serviceRPC   MicaService = "rpc"
	serviceUMT   MicaService = "umt"
	serviceDebug MicaService = "debug"
)

const (
	createMsgDebugFieldSize    = 1
	createMsgIntFieldSize      = 4
	createMsgIntFieldCount     = 6
	createMsgPrefixSize        = MaxNameLen + MaxFirmwarePathLen + MaxPedLen + MaxFirmwarePathLen + createMsgDebugFieldSize + MaxCPUStringLen
	createMsgPaddingAfterCPU   = (createMsgIntFieldSize - (createMsgPrefixSize % createMsgIntFieldSize)) % createMsgIntFieldSize
	createMsgPackedIntsSize    = createMsgIntFieldCount * createMsgIntFieldSize
	createMsgSerializedBufSize = createMsgPrefixSize + createMsgPaddingAfterCPU + createMsgPackedIntsSize + MaxConfigStrLen*2
)

const (
	MicaUpdateVCPU            MicaUpdateField = "VCPU"
	MicaUpdatePCPUConstraints MicaUpdateField = "CPU"
	// MicaUpdateCPUCapacity preserves the spelling expected by micad.
	MicaUpdateCPUCapacity   MicaUpdateField = "CPUCpacity"
	MicaUpdateCPUWeight     MicaUpdateField = "CPUWeight"
	MicaUpdateMemoryMax     MicaUpdateField = "MaxMem"
	MicaUpdateMemoryCurrent MicaUpdateField = "Memory"
)

func (f MicaUpdateField) Valid() bool {
	switch f {
	case MicaUpdateVCPU,
		MicaUpdatePCPUConstraints,
		MicaUpdateCPUCapacity,
		MicaUpdateCPUWeight,
		MicaUpdateMemoryMax,
		MicaUpdateMemoryCurrent:
		return true
	default:
		return false
	}
}

type micaCtlFunc func(context.Context, MicaCommand, string, ...string) error

type MicaExecutor struct {
	records           MicaClientConf
	ID                string
	memoryThresholdMB uint32
	Hypervisor        hypervisorControl
}

type hypervisorControl interface {
	Type() ports.HypervisorType
	MaxCPUNum(ctx context.Context) uint32
	SetMemory(ctx context.Context, id string, memMB uint32) error
	SetMaxMemory(ctx context.Context, id string, memMB uint32) error
	SetCPUWeight(ctx context.Context, id string, weight uint32) error
	SetCPUCapacity(ctx context.Context, id string, capacity uint32) error
	SetVCPUCount(ctx context.Context, id string, count uint32) error
	Pause(ctx context.Context, id string) error
	Resume(ctx context.Context, id string) error
}

// MicaUpdateRequest represents a structured resource update command for a MICA client.
// Replaces the ad-hoc strings.Join(cmdArgs, " ") pattern used in resource_manager.go.
type MicaUpdateRequest struct {
	Field MicaUpdateField
	Value string
}

// WireFormat returns the text representation sent to micad via the control socket.
func (r MicaUpdateRequest) WireFormat() string {
	return string(r.Field) + " " + r.Value
}
