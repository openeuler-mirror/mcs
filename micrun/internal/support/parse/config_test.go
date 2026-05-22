package parse

import (
	"os"
	"testing"

	"github.com/gookit/ini/v2"
)

const (
	simpleINIContent = `
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
)

var testCases = []struct {
	name    string
	content string
}{
	{"simple", simpleINIContent},
	{"large", largeINIContent},
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

func TestParseINITrimsSectionNames(t *testing.T) {
	path := createTestINIFile(t, `
[ static_resource ]
max_container_vcpu = 2
[ ignored ]
max_container_vcpu = 9
`)
	defer os.Remove(path)

	got, err := ParseINI(path, []string{"static_resource"})
	if err != nil {
		t.Fatalf("ParseINI returned error: %v", err)
	}
	if got["max_container_vcpu"] != "2" {
		t.Fatalf("max_container_vcpu = %q, want 2", got["max_container_vcpu"])
	}
}

func TestParseININormalizesWhitelistSections(t *testing.T) {
	path := createTestINIFile(t, `
[static_resource]
max_container_vcpu = 2
[ignored]
max_container_vcpu = 9
`)
	defer os.Remove(path)

	got, err := ParseINI(path, []string{" Static_Resource "})
	if err != nil {
		t.Fatalf("ParseINI returned error: %v", err)
	}
	if got["max_container_vcpu"] != "2" {
		t.Fatalf("max_container_vcpu = %q, want 2", got["max_container_vcpu"])
	}
}

func TestParseINIParsesGlobalKeysWithoutWhitelist(t *testing.T) {
	path := createTestINIFile(t, `
runtime = "micrun"
[default]
debug = true
`)
	defer os.Remove(path)

	got, err := ParseINI(path, nil)
	if err != nil {
		t.Fatalf("ParseINI returned error: %v", err)
	}
	if got["runtime"] != "micrun" {
		t.Fatalf("runtime = %q, want micrun", got["runtime"])
	}
	if got["debug"] != "true" {
		t.Fatalf("debug = %q, want true", got["debug"])
	}
}

func TestParseINISkipsGlobalKeysWithSectionWhitelist(t *testing.T) {
	path := createTestINIFile(t, `
runtime = "micrun"
[default]
debug = true
`)
	defer os.Remove(path)

	got, err := ParseINI(path, []string{"default"})
	if err != nil {
		t.Fatalf("ParseINI returned error: %v", err)
	}
	if _, ok := got["runtime"]; ok {
		t.Fatalf("runtime should not be parsed with section whitelist: %v", got)
	}
	if got["debug"] != "true" {
		t.Fatalf("debug = %q, want true", got["debug"])
	}
}

func BenchmarkParseINI(b *testing.B) {
	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			iniPath := createTestINIFile(b, tc.content)
			defer os.Remove(iniPath)

			b.Run("no_whitelist", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_, err := ParseINI(iniPath, nil)
					if err != nil {
						b.Fatalf("ParseINI failed: %v", err)
					}
				}
			})

			if tc.name == "large" {
				b.Run("with_whitelist", func(b *testing.B) {
					whiteList := []string{"section1", "section5", "section10"}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						_, err := ParseINI(iniPath, whiteList)
						if err != nil {
							b.Fatalf("ParseINI with whitelist failed: %v", err)
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
				i := ini.New()
				err := i.LoadExists(iniPath)
				if err != nil {
					b.Fatalf("ini.LoadExists failed: %v", err)
				}
			}
		})
	}
}
