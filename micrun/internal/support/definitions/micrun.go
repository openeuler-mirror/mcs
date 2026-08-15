package defs

import "time"

const (
	RuntimeName          = "mica"
	MicaSuccess          = "MICA-SUCCESS"
	MicaFailed           = "MICA-FAILED"
	MicaSocketName       = "mica-create.socket"
	MicaCreateSocketPath = MicaStateDir + "/" + MicaSocketName
	MicaSocketBufSize    = 512
	MicaSocketTimeout    = 5 * time.Second
	// MicaSocketLongTimeout is used for daemon operations that are
	// synchronous and potentially slow (create/start/rm: firmware loading,
	// domain create/destroy). The daemon does not respond until the
	// operation finishes, so a 5s cap would misreport slow operations as
	// timeouts and collide with retries.
	MicaSocketLongTimeout = 30 * time.Second

	DefaultPauseImage = "registry.k8s.io/pause"
	SandboxVersion    = 1

	IsMock           = false
	WorkaroundUpdate = true
)
