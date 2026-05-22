package oci

type RuntimeConfig struct {
	Debug       bool
	hostProfile HostProfile

	RuntimeResourceConfig
	RuntimeImageConfig
	RuntimeSandboxPolicy
	RuntimePathConfig
}

func NewRuntimeConfigWithHost(hostProfile HostProfile) *RuntimeConfig {
	hostProfile = normalizeHostProfile(hostProfile)
	cfg := RuntimeConfig{
		hostProfile:           hostProfile,
		RuntimeResourceConfig: defaultRuntimeResourceConfig(hostProfile),
		RuntimeImageConfig:    defaultRuntimeImageConfig(),
		RuntimeSandboxPolicy:  defaultRuntimeSandboxPolicy(),
		RuntimePathConfig:     defaultRuntimePathConfig(),
	}
	return &cfg
}

func (r *RuntimeConfig) host() HostProfile {
	if r == nil {
		return normalizeHostProfile(HostProfile{})
	}
	return normalizeHostProfile(r.hostProfile)
}
