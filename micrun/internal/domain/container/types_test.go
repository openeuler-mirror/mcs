package container

import (
	"testing"
)

func TestShareToWeight(t *testing.T) {
	tests := []struct {
		name   string
		shares uint64
		want   uint32
	}{
		{"zero shares returns default 256", 0, 256},
		{"1024 shares maps to 100", 1024, 100},
		{"512 shares maps to 50", 512, 50},
		{"2048 shares maps to 200", 2048, 200},
		{"very small shares (1) returns minimum 1", 1, 1},
		{"small shares (10) returns minimum 1", 10, 1},
		{"shares=10 yields weight < 1 so clamped to 1", 10, 1},
		{"large shares (10240) maps to 1000", 10240, 1000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShareToWeight(tt.shares)
			if got != tt.want {
				t.Errorf("ShareToWeight(%d) = %d, want %d", tt.shares, got, tt.want)
			}
		})
	}
}

func TestPedestalTypeString(t *testing.T) {
	tests := []struct {
		pt   PedestalType
		want string
	}{
		{PedestalXen, "xen"},
		{PedestalBaremetal, "baremetal"},
		{PedestalUnsupported, "unsupported"},
		{PedestalType(99), "unsupported"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.pt.String()
			if got != tt.want {
				t.Errorf("PedestalType(%d).String() = %q, want %q", tt.pt, got, tt.want)
			}
		})
	}
}

func TestParsePedestalType(t *testing.T) {
	tests := []struct {
		input string
		want  PedestalType
	}{
		{"xen", PedestalXen},
		{"XEN", PedestalXen},
		{"Xen", PedestalXen},
		{"baremetal", PedestalBaremetal},
		{"BareMetal", PedestalBaremetal},
		{"", PedestalXen},
		{"unknown", PedestalUnsupported},
		{"kvm", PedestalUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parsePedestalType(tt.input)
			if got != tt.want {
				t.Errorf("parsePedestalType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestPedestalTypeFromInt(t *testing.T) {
	tests := []struct {
		v    int
		want PedestalType
	}{
		{0, PedestalXen},
		{1, PedestalBaremetal},
		{2, PedestalUnsupported},
		{99, PedestalType(99)},
	}
	for _, tt := range tests {
		t.Run(string(rune('0'+tt.v)), func(t *testing.T) {
			got := PedestalTypeFromInt(tt.v)
			if got != tt.want {
				t.Errorf("PedestalTypeFromInt(%d) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}

func TestNewResourceChangesDefaults(t *testing.T) {
	rc := NewResourceChanges()

	if rc.VCPU == nil || *rc.VCPU != 1 {
		t.Errorf("NewResourceChanges().VCPU = %v, want 1", rc.VCPU)
	}
	if rc.CPUCapacity == nil || *rc.CPUCapacity != 0 {
		t.Errorf("NewResourceChanges().CPUCapacity = %v, want 0", rc.CPUCapacity)
	}
	if rc.CPUWeight == nil || *rc.CPUWeight != 0 {
		t.Errorf("NewResourceChanges().CPUWeight = %v, want 0", rc.CPUWeight)
	}
	if rc.ClientCPUSet != "" {
		t.Errorf("NewResourceChanges().ClientCPUSet = %q, want empty", rc.ClientCPUSet)
	}
	if rc.MemoryMaxMB != nil {
		t.Errorf("NewResourceChanges().MemoryMaxMB = %v, want nil", rc.MemoryMaxMB)
	}
	if rc.MemoryMinMB != 0 {
		t.Errorf("NewResourceChanges().MemoryMinMB = %d, want 0", rc.MemoryMinMB)
	}
}
