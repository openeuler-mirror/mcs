// Package libmica provides client functionality for interacting with the MICA daemon.
// TODO: using containerd socket utils
package libmica

import (
	"encoding/binary"
	"fmt"
	defs "micrun/definitions"
	er "micrun/errors"
	log "micrun/logger"
	"micrun/pkg/pedestal"
	"path/filepath"
	"strings"
)

// Type Definitions

type MicaCommand string
type PedType int
type MicaState string
type MicaService string

// Constants

const (
	MCreate MicaCommand = "create"
	MStart  MicaCommand = "start"
	MStop   MicaCommand = "stop"
	MRemove MicaCommand = "rm"
	MPause  MicaCommand = "pause"
	MResume MicaCommand = "resume"
	MStatus MicaCommand = "status"
	// miuca set <short_id> MemoryInMiB/CPUCapacity <value>
	MUpdate MicaCommand = "set"

	// TODO:
	// Mica message field length constants
	MaxNameLen         = 66
	MaxPedLen          = 16
	MaxFirmwarePathLen = 256
	MaxCPUStringLen    = 128
	MaxConfigStrLen    = 512
)

const (
	Xen PedType = iota
	ACRN
	FusionDock
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
	defaultMaxVCPUs     = 8
	fallbackMaxMemoryMB = defs.DefaultMinMemMB * 2
)

type micaCtlFunc func(MicaCommand, string, ...string) error

type MicaExecutor struct {
	records           MicaClientConf
	Id                string
	memoryThresholdMB uint32
}

// Structs and Methods

// MicaStatus represents the complete status of a MICA client.
// TODO: remove Raw field in the future for space saving
type MicaStatus struct {
	Name     string        `json:"name"`
	CPU      string        `json:"cpu"`
	State    MicaState     `json:"state"`
	Services []MicaService `json:"services"`
	Raw      string        `json:"raw"` // Original raw response
}

// string returns a string representation of MicaStatus
func (ms MicaStatus) string() string {
	return fmt.Sprintf("Name: %s, CPU: %s, State: %s, Services: %v",
		ms.Name, ms.CPU, ms.State, ms.Services)
}

// isRunning checks if the client is in running state
func (ms MicaStatus) isRunning() bool {
	return ms.State == running
}

// IsStopped checks if the client is in stopped state
func (ms MicaStatus) IsStopped() bool {
	return ms.State == stopped
}

// hasService checks if the client has a specific service
func (ms MicaStatus) hasService(service MicaService) bool {
	for _, s := range ms.Services {
		if s == service {
			return s == service
		}
	}
	return false
}

// isValid checks if the status contains valid information
func (ms MicaStatus) isValid() bool {
	return ms.Name != "" && isValidCPUString(ms.CPU) && ms.State != unknown
}

type mcsFS struct {
	Source  string   `json:"source"`
	Target  string   `json:"target"`
	Ped     PedType  `json:"ped"`
	OS      string   `json:"os"`
	Mounted bool     `json:"mounted"`
	Options []string `json:"options"`
}

// MicaClientConfCreateOptions is an intermediate layer to pass configurations to MicaClientConf
type MicaClientConfCreateOptions struct {
	CPU             string
	Name            string
	Path            string
	Ped             string
	PedCfg          string
	VCPUs           int
	CPUWeight       int
	CPUCapacity     int
	MemoryMB        int
	MaxVCPUs        int
	MemoryThreshold int
	IOMem           string
	Network         string
}

// This is the conf struct mica daemon will see
// Layout mirrors mica daemon's struct create_msg definition:
// #define MAX_NAME_LEN         66
// #define MAX_FIRMWARE_PATH_LEN 256
// #define MAX_CPUSTR_LEN       128
// #define MAX_IOMEM_LEN      512 // reserved for IOMEM
// #define MAX_NETWORK_LEN      512
//
//		struct create_msg {
//			/* required configs */
//			char name[MAX_NAME_LEN];
//			char path[MAX_FIRMWARE_PATH_LEN];
//			/* optional configs for MICA*/
//			char ped[MAX_NAME_LEN];
//			char ped_cfg[MAX_FIRMWARE_PATH_LEN];
//			bool debug;
//			/* optional configs for pedestal */
//			char cpu_str[MAX_CPUSTR_LEN];
//			int vcpu_num;            // 4
//	   /** NEW: max_vcpu_num */
//	   int max_vcpu_num;          // 4
//			int cpu_weight;          // 4
//			int cpu_capacity;        // 4
//			int memory;              // 4
//	   /** NEW: max_memory */
//	   int max_memory;            // 4
//	   /** NEW: iomem */
//		 char iomem[MAX_NETWORK_LEN]; // 512
//		 char network[MAX_NETWORK_LEN]; // 512
//		};
type MicaClientConf struct {
	// name is container ID, assigned by containerd.
	name [MaxNameLen]byte
	// path is the firmware path (<OS>.elf)
	path [MaxFirmwarePathLen]byte
	// ped is string of pedestal type: xen, fusionDock, acrn, etc.
	ped [MaxPedLen]byte
	// for xen, pedcfg is the relative path of <OS>.bin
	pedcfg [MaxFirmwarePathLen]byte
	// debug flag
	debug bool
	// cpuStr is the allowed cpu range => cpu=1-3,5
	cpuStr [MaxCPUStringLen]byte
	// vcpuNum is the number of vcpus
	vcpuNum int
	// TODO: micrun config set default maxVcpuNum (default of maxVcpuNum: 8)
	maxVcpuNum int
	// cpuWeight is the weight of cpu
	cpuWeight int
	// cpuCapacity is the capacity of cpu
	cpuCapacity int
	// memoryMB size in MiB
	memoryMB int
	// NOTICE: this is not maxmemory of container, it is the memory threshold of client, (default: 2 * memory)
	// memoryThresholdMB => maxmemory for mica conf
	memoryThresholdMB int
	// NOTICE:  reserved for iomem
	iomem [MaxConfigStrLen]byte
	// network config
	network [MaxConfigStrLen]byte
}

// dummyCPUArr is a dummy CPU array for testing, always [1,4,5]
func dummyCPUArr() []int {
	return []int{1, 4, 5}
}

// InitWithOpts initializes MicaClientConf with the new options struct
func (m *MicaClientConf) InitWithOpts(opts MicaClientConfCreateOptions) {
	*m = MicaClientConf{}
	if len(opts.Name) > MaxNameLen {
		log.Warnf("container name %q exceeds mica limit (%d); truncating for client registration", opts.Name, MaxNameLen)
	}
	copy(m.name[:], opts.Name)

	copy(m.path[:], opts.Path)

	copy(m.ped[:], opts.Ped)

	copy(m.pedcfg[:], opts.PedCfg)

	m.debug = false

	// Convert CPU array to string
	// cpuStr := pedestal.ParseCPUArr(opts.CPU)
	cpuStr := opts.CPU
	copy(m.cpuStr[:], cpuStr)

	// Set other fields
	m.vcpuNum = opts.VCPUs
	if opts.MaxVCPUs > 0 {
		m.maxVcpuNum = opts.MaxVCPUs
	} else {
		m.maxVcpuNum = defaultMaxVCPUs
	}
	m.cpuWeight = opts.CPUWeight
	m.cpuCapacity = opts.CPUCapacity
	m.memoryMB = opts.MemoryMB
	m.iomem = [MaxConfigStrLen]byte{}
	memInitThreshold := opts.MemoryThreshold
	if memInitThreshold == 0 {
		if opts.MemoryMB > 0 {
			memInitThreshold = opts.MemoryMB * 2
		} else {
			memInitThreshold = fallbackMaxMemoryMB
		}
	}
	m.memoryThresholdMB = memInitThreshold
	if opts.IOMem != "" {
		copy(m.iomem[:], opts.IOMem)
	}
	copy(m.network[:], opts.Network)

}

func (m *MicaClientConf) pack() []byte {
	// Serialized layout mirrors `struct create_msg` defined in micad.
	// See `mcs/mica/micad/socket_listener.c`.
	buf := make([]byte, createMsgSerializedBufSize)

	offset := 0
	copy(buf[offset:offset+MaxNameLen], m.name[:])
	offset += MaxNameLen
	copy(buf[offset:offset+MaxFirmwarePathLen], m.path[:])
	offset += MaxFirmwarePathLen
	copy(buf[offset:offset+MaxPedLen], m.ped[:])
	offset += MaxPedLen
	copy(buf[offset:offset+MaxFirmwarePathLen], m.pedcfg[:])
	offset += MaxFirmwarePathLen

	if m.debug {
		buf[offset] = 1
	} else {
		buf[offset] = 0
	}
	offset += 1

	copy(buf[offset:offset+MaxCPUStringLen], m.cpuStr[:])
	offset += MaxCPUStringLen

	// Align the next integer field with the C struct layout.
	offset += createMsgPaddingAfterCPU

	binary.LittleEndian.PutUint32(buf[offset:], uint32(m.vcpuNum))
	offset += createMsgIntFieldSize
	binary.LittleEndian.PutUint32(buf[offset:], uint32(m.maxVcpuNum))
	offset += createMsgIntFieldSize
	binary.LittleEndian.PutUint32(buf[offset:], uint32(m.cpuWeight))
	offset += createMsgIntFieldSize
	binary.LittleEndian.PutUint32(buf[offset:], uint32(m.cpuCapacity))
	offset += createMsgIntFieldSize
	binary.LittleEndian.PutUint32(buf[offset:], uint32(m.memoryMB))
	offset += createMsgIntFieldSize
	binary.LittleEndian.PutUint32(buf[offset:], uint32(m.memoryThresholdMB))
	offset += createMsgIntFieldSize
	copy(buf[offset:offset+MaxConfigStrLen], m.iomem[:])
	offset += MaxConfigStrLen
	copy(buf[offset:offset+MaxConfigStrLen], m.network[:])

	return buf
}

// Compatitble with status filter
type Filter struct {
	Name string
	Ped  bool
}

// Public API

func NewMicaCreateMsgWithOpts(opts MicaClientConfCreateOptions) MicaClientConf {
	msg := MicaClientConf{}
	msg.InitWithOpts(opts)
	return msg
}

// Create creates a new mica client.
// Use MicaCtl to control the mica client.
func Create(config MicaClientConf) error {
	s := newMicaSocket(defs.MicaCreatSocketPath)
	return s.handleMsg(config.pack())
}

func CreateMicaClient(conf MicaClientConf) error {
	s := newMicaSocket(defs.MicaCreatSocketPath)
	// Do not dereference s here, as it is dropped in handleMsg().
	msg := conf.pack()
	if err := s.handleMsg(msg); err != nil {
		return err
	}
	return nil
}

// TODO: consider better way to parse variable parameters
func micaCtlImpl(cmd MicaCommand, id string, opts ...string) error {
	if !validSocketPath(defs.MicaCreatSocketPath) {
		return er.MicadNotRunning
	}

	if id == "" {
		return fmt.Errorf("empty client id is not allowed")
	}

	if len(id) > MaxNameLen {
		return fmt.Errorf("client id %q exceeds mica limit (%d characters)", id, MaxNameLen)
	}

	// Debug branch for MUpdate: use xl commands instead of micad set command
	if cmd == MUpdate && defs.WorkaroundUpdate {
		log.Debug("calling xl to update resource for debug")
		if err := handleMicaUpdateWithXl(id, opts...); err != nil {
			log.Warnf("xl workaround failed: %v", err)
		} else {
			return nil
		}
	}

	clientSocketPath := filepath.Join(defs.MicaStateDir, id+".socket")
	s := newMicaSocket(clientSocketPath)

	// workaround: pause => stop
	switch cmd {
	case MPause:
		cmd = MStop
	case MResume:
		cmd = MStart
	case MStatus:
		s = newMicaSocket(defs.MicaCreatSocketPath)
	}
	msg := string(cmd)
	return s.handleMsg([]byte(msg))
}

var micaCtlFn micaCtlFunc = micaCtlImpl

func micaCtl(cmd MicaCommand, id string, opts ...string) error {
	return micaCtlFn(cmd, id, opts...)
}

func Start(id string) error {
	if err := micaCtl(MStart, id); err != nil {
		return fmt.Errorf("failed to start container %s", id)
	}
	return nil
}

// TODO: Extend mica response data, loading more information
// TODO: completely migrate remove to stop, currently use remove instead of stop
// we have to make sure that client os is down really
func Stop(id string) error {
	if ClientNotExist(id) {
		log.Infof("%s is already down, not need to stop it", id)
	} else if err := micaCtl(MRemove, id); err != nil {
		return fmt.Errorf("failed to stop mica client %s %w", id, err)
	}
	return nil
}

// TALK: xen supports pause, but mica...
// TODO: might passthrough mica, directly to ped?
func Pause(id string) error {
	if pedestal.GetHostPed() == pedestal.Xen {
		return pedestal.Pause(id)
	} else {
		if err := micaCtl(MPause, id); err != nil {
			return fmt.Errorf("failed to pause mica client %s %w", id, err)
		}
		return nil
	}
}

// TODO: mica may not support, we handle this via ped directly
func Resume(id string) error {
	if pedestal.GetHostPed() == pedestal.Xen {
		return pedestal.Resume(id)
	} else {
		if err := micaCtl(MResume, id); err != nil {
			return fmt.Errorf("failed to pause mica client %s %w", id, err)
		}
		return nil
	}
}

func Remove(id string) error {
	if ClientNotExist(id) {
		return nil
	}
	return micaCtl(MRemove, id)
}

// Status returns structured status information for a specific client
// TODO: support filter?
func Status(id string, filter Filter) (*MicaStatus, error) {
	res, err := queryStatus(id)
	if err != nil {
		return nil, fmt.Errorf("failed to get status for client %s: %v", id, err)
	}

	status, err := parseMicaStatus(res)
	if err != nil {
		return nil, fmt.Errorf("failed to parse status for client %s: %v", id, err)
	}

	if !status.isValid() {
		return nil, fmt.Errorf("invalid status for client %s: %s", id, status.Raw)
	}

	// Apply filter if specified
	if filter.Ped && !status.hasService(servicePTY) {
		return nil, fmt.Errorf("client %s does not have PTY service", id)
	}

	return status, nil
}

// StatusToString converts MicaStatus back to string format for backward compatibility
func StatusToString(status *MicaStatus) string {
	if status == nil {
		return ""
	}
	return status.Raw
}

// FilterStatuses filters a list of statuses based on criteria
func FilterStatuses(statuses []*MicaStatus, nameFilter string, stateFilter MicaState, serviceFilter MicaService) []*MicaStatus {
	var filtered []*MicaStatus

	for _, status := range statuses {
		// Name filter
		if nameFilter != "" && !strings.Contains(status.Name, nameFilter) {
			continue
		}

		// State filter
		if stateFilter != unknown && status.State != stateFilter {
			continue
		}

		// Service filter
		if serviceFilter != "" && !status.hasService(serviceFilter) {
			continue
		}

		filtered = append(filtered, status)
	}

	return filtered
}
