package pedestal

import "strings"

type PedType int

type PedestalConfig struct {
	PedType     PedType
	PedConfig   string
	MiniVCPUNum uint32
}

// Host is the global pedestal instance for the detected host pedestal type.
// Initialized at package startup via init().
var Host Pedestal

func init() {
	Host = newHostPed()
}

const (
	Xen PedType = iota
	FusionDock
	ACRN
	Baremetal
	Unsupported
)

// String returns the string representation of PedType
func (p PedType) String() string {
	switch p {
	case Xen:
		return "xen"
	case FusionDock:
		return "fusiondock"
	case ACRN:
		return "acrn"
	case Baremetal:
		return "baremetal"
	default:
		return "unsupported"
	}
}

func ParsePedType(s string) PedType {
	switch strings.ToLower(s) {
	case "xen":
		return Xen
	case "fusiondock":
		return FusionDock
	case "acrn":
		return ACRN
	case "baremetal", "openamp":
		return Baremetal
	case "":
		return Xen
	default:
		return Unsupported
	}
}

// New returns a Pedestal implementation for the given PedType.
func New(pedType PedType) Pedestal {
	switch pedType {
	case Xen:
		return xen{}
	case FusionDock:
		return fusiondock{}
	case ACRN:
		return acrn{}
	case Baremetal:
		return baremetal{}
	default:
		return DefaultPedestal{}
	}
}

// newHostPed returns a Pedestal implementation for the detected host pedestal type.
func newHostPed() Pedestal {
	return New(hostPed())
}
