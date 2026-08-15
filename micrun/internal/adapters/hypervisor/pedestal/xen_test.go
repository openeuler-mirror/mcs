package pedestal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShareToWeightFallbackAndClamp(t *testing.T) {
	tests := []struct {
		name   string
		shares uint64
		want   uint32
	}{
		{
			name:   "zero shares uses default",
			shares: 0,
			want:   DefaultXenWeight,
		},
		{
			name:   "standard cgroup shares",
			shares: 1024,
			want:   256,
		},
		{
			name:   "tiny share clamps to one",
			shares: 1,
			want:   1,
		},
		{
			name:   "large share clamps to max",
			shares: 65535*uint64(ShareWeightRatio) + 64,
			want:   65535,
		},
	}

	for _, tt := range tests {
		if got := ShareToWeight(tt.shares); got != tt.want {
			t.Errorf("%s: ShareToWeight(%d) = %d, want %d", tt.name, tt.shares, got, tt.want)
		}
	}
}

func TestParseXlInfo(t *testing.T) {
	output := `
host                   : qemu-aarch64
machine                : aarch64
nr_cpus                : 4
max_cpu_id             : 3
cores_per_socket       : 1
threads_per_core       : 1
cpu_mhz                : 62.500
total_memory           : 2048
free_memory            : 1536
free_cpus              : 2
xen_major              : 4
xen_minor              : 18
xen_extra              : .2
xen_scheduler          : credit2
xen_pagesize           : 4096
virt_caps              : hvm hap
`

	info, err := parseXlInfo(output)
	if err != nil {
		t.Fatalf("parseXlInfo returned error: %v", err)
	}

	if info.host != "qemu-aarch64" {
		t.Fatalf("host = %q, want qemu-aarch64", info.host)
	}
	if info.nrCpus != 4 || info.totalMemoryMB != 2048 || info.freeMemoryMB != 1536 {
		t.Fatalf("unexpected cpu/memory fields: %+v", info)
	}
	if info.xlver != "4.18.2" {
		t.Fatalf("xlver = %q, want 4.18.2", info.xlver)
	}
}

func TestParseXlVcpuInfo(t *testing.T) {
	output := `
Name                                ID  VCPU   CPU State   Time(s) Affinity (Hard / Soft)
Domain-0                             0     0    1   -b-     271.1  all / all
Domain-0                             0     1    -   ---       0.0  0-1 / all
mica-test                            7     0    2   r--      12.5  2 / 2
5527fc1be2e31d7aeaed7d22c2e9766df0dc92d15157241d82e65850033d0b4a    18     0    1   r--       3.1  all / all
`

	info, err := parseXlVcpuInfo(output)
	if err != nil {
		t.Fatalf("parseXlVcpuInfo returned error: %v", err)
	}

	if len(info.DomainVCPUMap["Domain-0"]) != 2 {
		t.Fatalf("Domain-0 vcpu count = %d, want 2", len(info.DomainVCPUMap["Domain-0"]))
	}

	entry := info.DomainVCPUMap["mica-test"][0]
	if entry.DomainID != 7 || entry.CPU != 2 || entry.State != "r--" {
		t.Fatalf("unexpected mica-test entry: %+v", entry)
	}

	offline := info.DomainVCPUMap["Domain-0"][1]
	if offline.CPU != -1 || offline.State != "---" {
		t.Fatalf("unexpected offline entry: %+v", offline)
	}

	longName := "5527fc1be2e31d7aeaed7d22c2e9766df0dc92d15157241d82e65850033d0b4a"
	longEntry := info.DomainVCPUMap[longName][0]
	if longEntry.DomainID != 18 || longEntry.TimeSeconds != 3.1 ||
		longEntry.HardAffinity != "all" || longEntry.SoftAffinity != "all" {
		t.Fatalf("unexpected long-name entry: %+v", longEntry)
	}
}

func TestXlInfoMemoryMBRejectsUnusableTotals(t *testing.T) {
	tests := []struct {
		name string
		info *XlInfo
		free uint32
		want uint32
		ok   bool
	}{
		{
			name: "nil info",
			info: nil,
		},
		{
			name: "zero total is a parse anomaly, not a memoryless host",
			info: &XlInfo{freeMemoryMB: 1536},
		},
		{
			name: "usable total",
			info: &XlInfo{freeMemoryMB: 1536, totalMemoryMB: 2048},
			free: 1536,
			want: 2048,
			ok:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			free, total, ok := xlInfoMemoryMB(tc.info)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if free != tc.free || total != tc.want {
				t.Fatalf("(free, total) = (%d, %d), want (%d, %d)", free, total, tc.free, tc.want)
			}
		})
	}
}

// A "no total_memory line" output must not make the runtime believe the host
// has no memory: MemHighThreshold would clamp to the 2 MiB floor and every
// configured container memory value would then fail the host-bounds check.
func TestParseXlInfoWithoutTotalMemoryIsNotUsable(t *testing.T) {
	info, err := parseXlInfo("host                   : qemu-aarch64\nnr_cpus                : 4\n")
	if err != nil {
		t.Fatalf("parseXlInfo returned error: %v", err)
	}
	if _, _, ok := xlInfoMemoryMB(info); ok {
		t.Fatal("xlInfoMemoryMB reported usable memory for output without total_memory")
	}
}

// xl stderr must reach the returned error: a domain that vanished between
// two xl calls reports the reason on stderr, and isMissingDomainError
// classifies on that text — a bare "exit status 1" turns a concurrent
// no-op destroy into a hard Stop/Delete failure.
func writeFakeXl(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "xl")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake xl: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestXlDestroySurfacesStderr(t *testing.T) {
	writeFakeXl(t, "#!/bin/sh\necho 'xl: destroy: unable to find domain ghost' >&2\nexit 1\n")

	err := xlDestroy(context.Background(), "ghost")
	if err == nil {
		t.Fatal("xlDestroy unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "unable to find domain") {
		t.Fatalf("stderr missing from error: %v", err)
	}
}

func TestXlListDomainStateSurfacesStderr(t *testing.T) {
	writeFakeXl(t, "#!/bin/sh\ncase \"$1\" in domid) echo 5; exit 0;; *) echo 'xl: list: domain 5 not found' >&2; exit 1;; esac\n")

	_, err := xlListDomainState(context.Background(), "ghost")
	if err == nil {
		t.Fatal("xlListDomainState unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("stderr missing from error: %v", err)
	}
}
