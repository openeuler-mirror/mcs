package oci

import (
	"os"

	log "micrun/internal/support/logger"
	"micrun/internal/support/parse"
)

type runtimeConfigParser func(string, []string) (parse.INI, error)

// ParseRuntimeFromINI loads micrun's scalar section/key runtime config format.
func (r *RuntimeConfig) ParseRuntimeFromINI(configPath string) error {
	return r.parseRuntimeFile(configPath, "ini", parse.ParseINI)
}

func (r *RuntimeConfig) ParseRuntimeFromToml(configPath string) error {
	return r.parseRuntimeFile(configPath, "toml", parse.ParseToml)
}

func (r *RuntimeConfig) parseRuntimeFile(configPath string, format string, parser runtimeConfigParser) error {
	if _, err := os.Stat(configPath); err != nil {
		return err
	}
	filtered, err := parser(configPath, runtimeConfigKeys)
	if err != nil {
		return err
	}

	log.Debugf("parsed runtime %s config: %v", format, filtered)
	r.applyRawConfig(filtered)
	return nil
}
