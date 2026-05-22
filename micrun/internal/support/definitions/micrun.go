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

	DefaultPauseImage = "registry.k8s.io/pause"
	SandboxVersion    = 1

	IsMock           = false
	WorkaroundUpdate = true
)
