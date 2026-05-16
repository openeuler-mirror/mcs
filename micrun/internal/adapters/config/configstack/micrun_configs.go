package configstack

import (
	"errors"
	"fmt"
	defs "micrun/internal/support/definitions"
	"micrun/internal/support/fs"
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

func DiscoverMicrunConfigFiles() ([]MicrunConfigFile, error) {
	if override := FirstNonEmptyEnv(defs.MicrunConfEnv); override != "" {
		f, err := makeConfigFile(override)
		if err != nil {
			return nil, err
		}
		return []MicrunConfigFile{f}, nil
	}

	if dirByEnv := FirstNonEmptyEnv(defs.MicrunConfDirEnv); dirByEnv != "" {
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

	f, err := makeConfigFile(defaultConfigFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
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

func MicrunConfigFileFromPath(path string) (MicrunConfigFile, error) {
	return makeConfigFile(path)
}

func makeConfigFile(path string) (MicrunConfigFile, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return MicrunConfigFile{}, fmt.Errorf("micrun config path is required")
	}
	cleanPath, err := fs.CleanAbsolutePath(path)
	if err != nil {
		return MicrunConfigFile{}, fmt.Errorf("micrun config path is invalid: %w", err)
	}
	info, err := os.Stat(cleanPath)
	if err != nil {
		return MicrunConfigFile{}, err
	}
	if !info.Mode().IsRegular() {
		return MicrunConfigFile{}, fmt.Errorf("micrun config %s is not a regular file", cleanPath)
	}
	format := detectConfigFormat(cleanPath)
	if format == FormatUnknown {
		return MicrunConfigFile{}, fmt.Errorf("unsupported micrun config extension: %s, should be .ini, .conf or .toml for toml", cleanPath)
	}
	return MicrunConfigFile{Path: cleanPath, Format: format}, nil
}

func listMicrunConfigDir(dir string) ([]MicrunConfigFile, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, nil
	}
	cleanDir, err := fs.CleanAbsolutePath(dir)
	if err != nil {
		return nil, fmt.Errorf("micrun config directory is invalid: %w", err)
	}
	entries, err := os.ReadDir(cleanDir)
	if err != nil {
		return nil, err
	}

	files := make([]MicrunConfigFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		full := filepath.Join(cleanDir, entry.Name())
		if !fs.IsRegular(full) {
			continue
		}
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
