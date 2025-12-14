//go:build test
// +build test

package utils

import (
	"os"
	"testing"

	"github.com/gookit/ini/v2"
)

const (
	simpleINIContent = `
; Simple test ini file
[default]
name = "app"
debug = true
[database]
host = "localhost"
port = 5432
`
	largeINIContent = `
[section1]
key1 = value1
key2 = value2
[section2]
key1 = value1
key2 = value2
[section3]
key1 = value1
key2 = value2
[section4]
key1 = value1
key2 = value2
[section5]
key1 = value1
key2 = value2
[section6]
key1 = value1
key2 = value2
[section7]
key1 = value1
key2 = value2
[section8]
key1 = value1
key2 = value2
[section9]
key1 = value1
key2 = value2
[section10]
key1 = value1
key2 = value2
`
	mixedQuotesINIContent = `
[service]
name = "test_service"
path = '"/usr/local/bin"'
args = '["--config", "/etc/service.conf"]'
[service]
name = "test_service"
path = '"/usr/local/bin"'
args = '["--config", "/etc/service.conf"]'
[service]
name = "test_service"
path = '"/usr/local/bin"'
args = '["--config", "/etc/service.conf"]'
[service]
name = "test_service"
path = '"/usr/local/bin"'
args = '["--config", "/etc/service.conf"]'
`
	edgeCasesINIContent = `
[empty_section]

[section_with_empty_values]
key1 =
key2=
key3 = ""
key4 = ''

[ a bit weird section name ]
key = value
`
)

var testCases = []struct {
	name    string
	content string
}{
	{"simple", simpleINIContent},
	{"large", largeINIContent},
	{"mixedQuotes", mixedQuotesINIContent},
	{"edgeCases", edgeCasesINIContent},
}

func createTestINIFile(tb testing.TB, content string) string {
	tb.Helper()
	file, err := os.CreateTemp("", "test-*.ini")
	if err != nil {
		tb.Fatalf("Failed to create temp file: %v", err)
	}
	defer file.Close()

	_, err = file.WriteString(content)
	if err != nil {
		tb.Fatalf("Failed to write to temp file: %v", err)
	}

	return file.Name()
}

func BenchmarkParseConfigINI(b *testing.B) {
	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			iniPath := createTestINIFile(b, tc.content)
			defer os.Remove(iniPath)

			b.Run("no_whitelist", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_, err := ParseConfigINI(iniPath, nil)
					if err != nil {
						b.Fatalf("ParseConfigINI failed: %v", err)
					}
				}
			})

			// Only run whitelist test for the 'large' case to see the performance difference
			if tc.name == "large" {
				b.Run("with_whitelist", func(b *testing.B) {
					whiteList := []string{"section1", "section5", "section10"}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						_, err := ParseConfigINI(iniPath, whiteList)
						if err != nil {
							b.Fatalf("ParseConfigINI with whitelist failed: %v", err)
						}
					}
				})
			}
		})
	}
}

func BenchmarkGookitIniLoad(b *testing.B) {
	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			iniPath := createTestINIFile(b, tc.content)
			defer os.Remove(iniPath)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Create a new instance and load the INI file.
				i := ini.New()
				err := i.LoadExists(iniPath)
				if err != nil {
					b.Fatalf("ini.LoadExists failed: %v", err)
				}
			}
		})
	}
}
