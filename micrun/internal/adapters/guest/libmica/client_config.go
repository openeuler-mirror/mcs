package libmica

import (
	"encoding/binary"

	defs "micrun/internal/support/definitions"
	log "micrun/internal/support/logger"
)

// MicaClientConfCreateOptions is an intermediate layer to pass configurations to MicaClientConf
type MicaClientConfCreateOptions struct {
	CPU             string
	Name            string
	Path            string
	Ped             string
	PedCfg          string
	VCPUs           uint32
	CPUWeight       uint32
	CPUCapacity     uint32
	MemoryMB        uint32
	MaxVCPUs        uint32
	MemoryThreshold uint32
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
	// ped is string of pedestal type: xen, baremetal, etc.
	ped [MaxPedLen]byte
	// for xen, pedcfg is the relative path of <OS>.bin
	pedcfg [MaxFirmwarePathLen]byte
	// debug flag
	debug bool
	// cpuStr is the allowed cpu range => cpu=1-3,5
	cpuStr [MaxCPUStringLen]byte
	// vcpuNum is the number of vcpus
	vcpuNum uint32
	// maxVcpuNum is supplied by runtime config and falls back to micad's legacy default.
	maxVcpuNum uint32
	// cpuWeight is the weight of cpu
	cpuWeight uint32
	// cpuCapacity is the capacity of cpu
	cpuCapacity uint32
	// memoryMB size in MiB
	memoryMB uint32
	// NOTICE: this is not maxmemory of container, it is the memory threshold of client, (default: 2 * memory)
	// memoryThresholdMB => maxmemory for mica conf
	memoryThresholdMB uint32
	// NOTICE:  reserved for iomem
	iomem [MaxConfigStrLen]byte
	// network config
	network [MaxConfigStrLen]byte
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

	cpuStr := opts.CPU
	copy(m.cpuStr[:], cpuStr)

	m.vcpuNum = opts.VCPUs
	m.maxVcpuNum = maxVCPUsOrDefault(opts.MaxVCPUs)
	m.cpuWeight = opts.CPUWeight
	m.cpuCapacity = opts.CPUCapacity
	m.memoryMB = opts.MemoryMB
	m.iomem = [MaxConfigStrLen]byte{}
	// On ARM64, Xen requires maxmem == memory (no Populate-on-Demand support)
	// So we set memoryThresholdMB equal to memoryMB to ensure maxmem == memory
	m.memoryThresholdMB = m.memoryMB
	if opts.IOMem != "" {
		copy(m.iomem[:], opts.IOMem)
	}
	copy(m.network[:], opts.Network)
}

func maxVCPUsOrDefault(maxVCPUs uint32) uint32 {
	if maxVCPUs > 0 {
		return maxVCPUs
	}
	return defs.DefaultMaxVCPUs
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

	binary.LittleEndian.PutUint32(buf[offset:], m.vcpuNum)
	offset += createMsgIntFieldSize
	binary.LittleEndian.PutUint32(buf[offset:], m.maxVcpuNum)
	offset += createMsgIntFieldSize
	binary.LittleEndian.PutUint32(buf[offset:], m.cpuWeight)
	offset += createMsgIntFieldSize
	binary.LittleEndian.PutUint32(buf[offset:], m.cpuCapacity)
	offset += createMsgIntFieldSize
	binary.LittleEndian.PutUint32(buf[offset:], m.memoryMB)
	offset += createMsgIntFieldSize
	binary.LittleEndian.PutUint32(buf[offset:], m.memoryThresholdMB)
	offset += createMsgIntFieldSize
	copy(buf[offset:offset+MaxConfigStrLen], m.iomem[:])
	offset += MaxConfigStrLen
	copy(buf[offset:offset+MaxConfigStrLen], m.network[:])

	return buf
}
