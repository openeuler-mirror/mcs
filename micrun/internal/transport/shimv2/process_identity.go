package shim

import "os"

type processIDProvider interface {
	PID() uint32
}

type osProcessIDProvider struct{}

func (osProcessIDProvider) PID() uint32 {
	return uint32(os.Getpid())
}

func processIDProviderFrom(deps runtimeDependencies) processIDProvider {
	if deps.processID != nil {
		return deps.processID
	}
	return osProcessIDProvider{}
}
