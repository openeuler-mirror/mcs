package micantainer

import (
	log "micrun/logger"
	ped "micrun/pkg/pedestal"
)

var (
	HostPedType ped.PedType
)

func init() {
	HostPedType = ped.Host.Type()
	if HostPedType == ped.Unsupported {
		log.Warnf("unsupported host ped type")
	}
}
