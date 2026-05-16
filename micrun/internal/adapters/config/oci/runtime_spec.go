package oci

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func parseConfigJSON(file string) (specs.Spec, error) {
	configBytes, err := os.ReadFile(file)
	if err != nil {
		return specs.Spec{}, err
	}

	var config specs.Spec
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return specs.Spec{}, err
	}

	return config, nil
}

func LoadSpec(bundle string) (specs.Spec, error) {
	configPath := filepath.Join(bundle, "config.json")
	return parseConfigJSON(configPath)
}
