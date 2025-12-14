package utils

import (
	"bufio"
	"fmt"
	log "micrun/logger"
	"os"
	"strings"
)

// stripQuotes removes surrounding quotes from a string if both start and end quotes match.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// filter for non-empty lines
type sectionFilter func(string) bool

// ParseINI performs a faster INI parsing method by reading line by line.
// with a simple lowercase section title filter
func ParseINI(configPath string, whiteList []string) (map[string]string, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open mica config file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	sectionAllowed := false
	wildcard := false
	if whiteList == nil {
		wildcard = true
	}
	parsedFields := make(map[string]string, 8)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// comments or empty line
		if len(line) == 0 || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' && line[len(line)-1] == ']' {
			sectionName := strings.ToLower(line[1 : len(line)-1])
			sectionAllowed = wildcard || InList(whiteList, sectionName)
			continue
		}

		if !sectionAllowed {
			continue
		}

		// find the separator (= or :)
		// NOTICE: for a=b:c, "b:c" will be considered as a value
		sepIndex := strings.IndexByte(line, '=')
		if sepIndex == -1 {
			sepIndex = strings.IndexByte(line, ':')
		}
		if sepIndex == -1 {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(line[:sepIndex]))
		value := strings.TrimSpace(line[sepIndex+1:])

		// remove surrounding quotes if present
		value = stripQuotes(value)

		parsedFields[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading mica config file: %v", err)
	}

	log.Pretty("parsed ini conf: %v", parsedFields)

	return parsedFields, nil
}

func ParseToml(configPath string, whiteList []string) (map[string]string, error) {
	filtered := make(map[string]string)
	return filtered, nil
}
