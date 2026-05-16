package pedestal

import "context"

// This file contains Xen-specific methods for PedestalFacade.
// These methods are available on all platforms but return ErrNotSupported
// when not running on Xen.

// DomainState reads the domain state from xenstore.
// Returns "running" if domain is active and ready, otherwise returns the actual state.
// Returns ErrNotSupported if not running on Xen.
func (f *PedestalFacade) DomainState(ctx context.Context, clientID string) (string, error) {
	if f.impl.Type() != Xen {
		return "", ErrNotSupported
	}
	return xenStoreReadDomainState(ctx, clientID)
}

// ConsolePath resolves the PTY path published by xl console for a given domain.
// Returns ErrNotSupported if not running on Xen.
func (f *PedestalFacade) ConsolePath(ctx context.Context, clientID string) (string, error) {
	if f.impl.Type() != Xen {
		return "", ErrNotSupported
	}
	return consolePTYPathForDomain(ctx, clientID)
}

// VCPUList returns the VCPU information for all domains.
// Returns ErrNotSupported if not running on Xen.
func (f *PedestalFacade) VCPUList(ctx context.Context) (*XlVcpuInfo, error) {
	if f.impl.Type() != Xen {
		return nil, ErrNotSupported
	}
	return xlVcpuList(ctx)
}

// DomainID returns the Xen domain ID for a client.
// Returns ErrNotSupported if not running on Xen.
func (f *PedestalFacade) DomainID(ctx context.Context, clientID string) (int, error) {
	if f.impl.Type() != Xen {
		return 0, ErrNotSupported
	}
	return domainID(ctx, clientID)
}

// SetVCPUCount sets the number of VCPUs for a domain.
// Returns ErrNotSupported if not running on Xen.
func (f *PedestalFacade) SetVCPUCount(ctx context.Context, clientID string, count uint32) error {
	if f.impl.Type() != Xen {
		return ErrNotSupported
	}
	return xlVcpuSet(ctx, clientID, int(count))
}
