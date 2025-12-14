package pedestal

import "strings"

type PedType int

type PedestalConfig struct {
	PedType     PedType
	PedConfig   string
	MiniVCPUNum uint32
}

const (
	Xen PedType = iota
	FusionDock
	ACRN
	OpenAMP
	Unsupported
)

// String returns the string representation of PedType
func (p PedType) String() string {
	switch p {
	case Xen:
		return "xen"
	default:
		return "unknown"
	}
}

func ParsePedType(s string) PedType {
	switch strings.ToLower(s) {
	case "xen", "":
		return Xen
	default:
		return Unsupported // default to baremetal
	}
}
