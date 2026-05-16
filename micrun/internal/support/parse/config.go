package parse

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	log "micrun/internal/support/logger"
)

type INI map[string]string

func ParseINI(configPath string, whiteList []string) (INI, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open mica config file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	sectionFilter := newSectionFilter(whiteList)
	sectionAllowed := sectionFilter.allows("")
	parsedFields := make(map[string]string, 8)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if len(line) == 0 || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' && line[len(line)-1] == ']' {
			sectionName := strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			sectionAllowed = sectionFilter.allows(sectionName)
			continue
		}

		if !sectionAllowed {
			continue
		}

		sepIndex := strings.IndexByte(line, '=')
		if sepIndex == -1 {
			sepIndex = strings.IndexByte(line, ':')
		}
		if sepIndex == -1 {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(line[:sepIndex]))
		value := strings.TrimSpace(line[sepIndex+1:])

		value = stripQuotes(value)

		parsedFields[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading mica config file: %w", err)
	}

	log.Pretty("parsed ini conf: %v", parsedFields)

	return parsedFields, nil
}

func ParseToml(configPath string, whiteList []string) (INI, error) {
	// Micrun runtime TOML uses the same scalar [section] key=value shape as the
	// existing INI config. Keep the parser narrow until richer TOML types are
	// needed by runtime config.
	return ParseINI(configPath, whiteList)
}

func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

type sectionFilter struct {
	wildcard bool
	allowed  map[string]struct{}
}

func newSectionFilter(whiteList []string) sectionFilter {
	if whiteList == nil {
		return sectionFilter{wildcard: true}
	}
	allowed := make(map[string]struct{}, len(whiteList))
	for _, section := range whiteList {
		allowed[strings.ToLower(strings.TrimSpace(section))] = struct{}{}
	}
	return sectionFilter{allowed: allowed}
}

func (f sectionFilter) allows(section string) bool {
	if f.wildcard {
		return true
	}
	_, ok := f.allowed[strings.ToLower(strings.TrimSpace(section))]
	return ok
}
