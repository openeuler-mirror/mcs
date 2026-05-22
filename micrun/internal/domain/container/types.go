package container

import (
	"strings"
)

// PedestalType represents the underlying hypervisor/platform type.
// This is a domain-level mirror of the adapter-layer PedType,
// ensuring the domain layer does not depend on adapter packages.
type PedestalType int

const (
	PedestalXen PedestalType = iota
	PedestalBaremetal
	PedestalUnsupported
)

func (p PedestalType) String() string {
	switch p {
	case PedestalXen:
		return "xen"
	case PedestalBaremetal:
		return "baremetal"
	default:
		return "unsupported"
	}
}

func parsePedestalType(s string) PedestalType {
	switch strings.ToLower(s) {
	case "xen":
		return PedestalXen
	case "baremetal":
		return PedestalBaremetal
	case "":
		return PedestalXen
	default:
		return PedestalUnsupported
	}
}

func PedestalTypeFromInt(v int) PedestalType {
	return PedestalType(v)
}

// ResourceChanges describes the resource deltas to apply to a container.
// This is a domain-level mirror of the adapter-layer EssentialResource,
// ensuring the domain layer does not depend on adapter packages.
type ResourceChanges struct {
	CPUCapacity  *uint32
	CPUWeight    *uint32
	ClientCPUSet string
	VCPU         *uint32
	MemoryMaxMB  *uint32
	MemoryMinMB  uint32
}

// NewResourceChanges returns zero resource changes with sensible defaults,
// mirroring the adapter-layer InitResource().
func NewResourceChanges() *ResourceChanges {
	vcpu := uint32(1)
	capacity := uint32(0)
	weight := uint32(0)
	return &ResourceChanges{
		VCPU:        &vcpu,
		CPUCapacity: &capacity,
		CPUWeight:   &weight,
	}
}

// VCPUUsageEntry represents per-vCPU timing information for stats.
type VCPUUsageEntry struct {
	TimeSeconds float64
}

// VCPUUsageInfo holds domain-to-vCPU-entry mapping for CPU stats.
type VCPUUsageInfo struct {
	DomainVCPUMap map[string][]VCPUUsageEntry
}

// ShareToWeight converts CPU shares to a weight value.
func ShareToWeight(shares uint64) uint32 {
	if shares == 0 {
		return 256
	}
	weight := shares * 100 / 1024
	if weight == 0 {
		return 1
	}
	return uint32(weight)
}

type PedestalConfig struct {
	PedType     PedestalType
	PedConfig   string
	MiniVCPUNum uint32
}

type GuestClientConfig struct {
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
}
