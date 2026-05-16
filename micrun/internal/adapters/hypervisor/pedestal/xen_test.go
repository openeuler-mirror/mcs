package pedestal

import "testing"

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
