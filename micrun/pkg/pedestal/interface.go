package pedestal

// TODO: use interface to handle so many different pedestal
type PedTraits interface {
	ToString() string
	GeneratePedConf() (string, error)
	// only support pinning all vcpu to another cpuset
	PinVCPU(clientID, cpus string)
	MemLowThreshold() uint32
	MemHighThreshold() uint32
}
