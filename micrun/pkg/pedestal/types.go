package pedestal

import "strings"

type PedType int

type PedestalConfig struct {
	PedType     PedType
	PedConfig   string
	MiniVCPUNum uint32
}

// Host is the global pedestal facade instance for the detected host pedestal type.
// Initialized at package startup via init().
// Use Host to access all pedestal operations without type assertions.
var Host *PedestalFacade

func init() {
	Host = NewPedestalFacade(newHostPed())
}

const (
	Xen PedType = iota
	Baremetal
	Unsupported
)

// String returns the string representation of PedType
func (p PedType) String() string {
	switch p {
	case Xen:
		return "xen"
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
	case "baremetal":
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
