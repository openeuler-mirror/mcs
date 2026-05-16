package oci

import (
	"strings"

	defs "micrun/internal/support/definitions"
)

type RuntimeImageConfig struct {
	ImagePath           string
	AuxFilePath         string
	PauseImage          string
	DefaultFirmwarePath string
}

func defaultRuntimeImageConfig() RuntimeImageConfig {
	return RuntimeImageConfig{
		PauseImage: defs.DefaultPauseImage,
	}
}

func (r *RuntimeConfig) SetPauseImage(pauseImage string) {
	trimmed := strings.TrimSpace(pauseImage)
	if trimmed == "" {
		return
	}
	r.PauseImage = trimmed
}

func (r *RuntimeConfig) SetDefaultFirmwarePath(path string) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return
	}
	r.DefaultFirmwarePath = trimmed
}
