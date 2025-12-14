package configstack

import (
	"path/filepath"
	"strings"

	er "micrun/errors"
	log "micrun/logger"
	"micrun/pkg/utils"
)

const clientConfName = "client.conf"

// ContainerLayer captures overrides sourced from client.conf or similar files.
type ContainerLayer struct {
	ImageAbsPath string
	PedestalType string
	PedestalConf string
	OS           string
}

// FallbackConfLayer parses bundleRootfs/client.conf and returns overrides, if any.
// should be the final fallback, if no other overrides are found.
// mirun-image-builder may not include client.conf file in bundle due to size concerns
func FallbackConfLayer(dir string) (ContainerLayer, error) {
	var layer ContainerLayer
	clientConf := filepath.Join(dir, clientConfName)
	if strings.TrimSpace(dir) == "" || !utils.FileExist(clientConf) {
		return layer, er.Missing
	}

	// whitelist the [Mica] section; utils.ParseConfigINI lowercases section names.
	fields, err := utils.ParseINI(clientConf, []string{"mica"})
	if err != nil {
		log.Warnf("failed to parse %s: %v; continuing without overrides", clientConf, err)
		return layer, nil
	}
	if len(fields) == 0 {
		return layer, er.Missing
	}

	if v := strings.TrimSpace(fields["clientpath"]); v != "" {
		layer.ImageAbsPath = v
	}
	if v := strings.TrimSpace(fields["pedestal"]); v != "" {
		layer.PedestalType = v
	}
	if v := strings.TrimSpace(fields["pedestalconf"]); v != "" {
		layer.PedestalConf = v
	}
	if v := strings.TrimSpace(fields["os"]); v != "" {
		layer.OS = v
	}

	log.Debugf("client.conf overrides resolved: %+v", layer)
	return layer, nil
}
