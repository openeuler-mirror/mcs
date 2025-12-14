package configstack

import (
	"errors"
	"fmt"
	defs "micrun/definitions"
	"micrun/pkg/utils"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ConfigFormat int

const (
	FormatUnknown ConfigFormat = iota
	FormatINI
	FormatTOML
)

type MicrunConfigFile struct {
	Path   string
	Format ConfigFormat
}

var (
	defaultDropinSearch = []string{defs.MicrunConfDropin}
	defaultConfigFile   = filepath.Join(defs.MicrunConfDir, defs.DefaultMicrunConf)
)

// priority env::file > env::dropin_dir > default::dropin_dir > default config file
func DiscoverMicrunConfigFiles() ([]MicrunConfigFile, error) {
	if override := os.Getenv(defs.MicrunConfEnv); override != "" {
		f, err := makeConfigFile(override)
		if err != nil {
			return nil, err
		}
		return []MicrunConfigFile{f}, nil
	}

	if dirByEnv := os.Getenv(defs.MicrunConfDirEnv); dirByEnv != "" {
		files, err := listMicrunConfigDir(dirByEnv)
		if err != nil {
			return nil, err
		}
		if len(files) > 0 {
			return files, nil
		}
	}

	var aggregated []MicrunConfigFile
	for _, dir := range defaultDropinSearch {
		files, err := listMicrunConfigDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		aggregated = append(aggregated, files...)
	}
	if len(aggregated) > 0 {
		return aggregated, nil
	}

	if !utils.FileExist(defaultConfigFile) {
		return nil, nil
	}

	f, err := makeConfigFile(defaultConfigFile)
	if errors.Is(err, os.ErrNotExist) {
	}
	if err != nil {
		return nil, err
	}

	return []MicrunConfigFile{f}, nil
}

func FirstNonEmptyEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func makeConfigFile(path string) (MicrunConfigFile, error) {
	if !utils.IsRegular(path) {
		return MicrunConfigFile{}, fmt.Errorf("micrun config %s is not a regular file or failed to stat it", path)
	}
	format := detectConfigFormat(path)
	if format == FormatUnknown {
		return MicrunConfigFile{}, fmt.Errorf("unsupported micrun config extension: %s, should be .ini, .conf or .toml for toml", path)
	}
	return MicrunConfigFile{Path: path, Format: format}, nil
}

func listMicrunConfigDir(dir string) ([]MicrunConfigFile, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []MicrunConfigFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		full := filepath.Join(dir, entry.Name())
		format := detectConfigFormat(full)
		if format == FormatUnknown {
			continue
		}
		files = append(files, MicrunConfigFile{Path: full, Format: format})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files, nil
}

func detectConfigFormat(path string) ConfigFormat {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ini", ".conf":
		return FormatINI
	case ".toml":
		return FormatTOML
	default:
		return FormatUnknown
	}
}
