package oci

import (
	"context"

	"micrun/internal/adapters/hypervisor/pedestal"
	"micrun/internal/support/contextx"
)

// HostProfile captures the platform defaults the OCI adapter needs, without
// reaching back into the global pedestal facade on every call path.
type HostProfile struct {
	Type             pedestal.PedType
	MemLowThreshold  uint32
	MemHighThreshold uint32
	HugePageSupport  func(staticResource bool) bool
}

func HostProfileFromFacade(ctx context.Context, host *pedestal.PedestalFacade) HostProfile {
	ctx = contextx.OrBackground(ctx)
	if host == nil {
		return normalizeHostProfile(HostProfile{})
	}

	return normalizeHostProfile(HostProfile{
		Type:             host.Type(),
		MemLowThreshold:  host.MemLowThreshold(),
		MemHighThreshold: host.MemHighThreshold(ctx),
		HugePageSupport:  host.HugePageSupport,
	})
}

func normalizeHostProfile(profile HostProfile) HostProfile {
	if profile.MemLowThreshold == 0 {
		profile.MemLowThreshold = 2
	}
	if profile.MemHighThreshold < profile.MemLowThreshold {
		profile.MemHighThreshold = profile.MemLowThreshold
	}
	if profile.HugePageSupport == nil {
		profile.HugePageSupport = func(bool) bool { return false }
	}
	return profile
}

func (p HostProfile) staticResourceDefault() bool {
	return normalizeHostProfile(p).Type == pedestal.Baremetal
}

func (p HostProfile) hugePageEnabled(staticResource bool) bool {
	return normalizeHostProfile(p).HugePageSupport(staticResource)
}
