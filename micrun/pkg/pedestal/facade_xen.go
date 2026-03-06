package pedestal

// This file contains Xen-specific methods for PedestalFacade.
// These methods are available on all platforms but return ErrNotSupported
// when not running on Xen.

// DomainState reads the domain state from xenstore.
// Returns "running" if domain is active and ready, otherwise returns the actual state.
// Returns ErrNotSupported if not running on Xen.
func (f *PedestalFacade) DomainState(clientID string) (string, error) {
	if f.impl.Type() != Xen {
		return "", ErrNotSupported
	}
	return xenStoreReadDomainState(clientID)
}

// ConsolePath resolves the PTY path published by xl console for a given domain.
// Returns ErrNotSupported if not running on Xen.
func (f *PedestalFacade) ConsolePath(clientID string) (string, error) {
	if f.impl.Type() != Xen {
		return "", ErrNotSupported
	}
	return consolePTYPathForDomain(clientID)
}

// VCPUList returns the VCPU information for all domains.
// Returns ErrNotSupported if not running on Xen.
func (f *PedestalFacade) VCPUList() (*XlVcpuInfo, error) {
	if f.impl.Type() != Xen {
		return nil, ErrNotSupported
	}
	return xlVcpuList()
}

// DomainID returns the Xen domain ID for a client.
// Returns ErrNotSupported if not running on Xen.
func (f *PedestalFacade) DomainID(clientID string) (int, error) {
	if f.impl.Type() != Xen {
		return 0, ErrNotSupported
	}
	return domainID(clientID)
}

// SetVCPUCount sets the number of VCPUs for a domain.
// Returns ErrNotSupported if not running on Xen.
func (f *PedestalFacade) SetVCPUCount(clientID string, count uint32) error {
	if f.impl.Type() != Xen {
		return ErrNotSupported
	}
	return xlVcpuSet(clientID, int(count))
}
