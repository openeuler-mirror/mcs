package libmica

import (
	"context"
	"testing"
)

// newTestExecutor creates a MicaExecutor with pre-populated records for
// NeedUpdate* heuristic testing. No socket or hypervisor is wired — the
// Need* methods only read in-memory state.
func newTestExecutor(id string, memoryMB, thresholdMB, vcpus, cpuWeight, cpuCap uint32, cpuStr string) *MicaExecutor {
	me := &MicaExecutor{
		ID:                id,
		memoryThresholdMB: thresholdMB,
	}
	copy(me.records.name[:], id)
	me.records.memoryMB = memoryMB
	me.records.vcpuNum = vcpus
	me.records.cpuWeight = cpuWeight
	me.records.cpuCapacity = cpuCap
	copy(me.records.cpuStr[:], cpuStr)
	return me
}

func TestNeedUpdateMemLimit(t *testing.T) {
	cases := []struct {
		name    string
		current uint32 // records.memoryMB
		target  uint32
		want    bool
	}{
		{"same value", 128, 128, false},
		{"increase", 128, 256, true},
		{"decrease", 256, 128, true},
		{"zero current nonzero target", 0, 64, true},
		{"nonzero current zero target", 64, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			me := newTestExecutor("test", tc.current, 0, 0, 0, 0, "")
			if got := me.NeedUpdateMemLimit(tc.target); got != tc.want {
				t.Errorf("NeedUpdateMemLimit(current=%d, target=%d) = %v, want %v",
					tc.current, tc.target, got, tc.want)
			}
		})
	}
}

func TestNeedUpdateMemThreshold(t *testing.T) {
	cases := []struct {
		name      string
		threshold uint32
		target    uint32
		want      bool
	}{
		{"threshold below target", 128, 256, true},
		{"threshold equals target", 256, 256, false},
		{"threshold above target (monotonic increase only)", 512, 256, false},
		{"zero threshold nonzero target", 0, 64, true},
		{"nonzero threshold zero target", 64, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			me := newTestExecutor("test", 0, tc.threshold, 0, 0, 0, "")
			if got := me.NeedUpdateMemThreshold(tc.target); got != tc.want {
				t.Errorf("NeedUpdateMemThreshold(threshold=%d, target=%d) = %v, want %v",
					tc.threshold, tc.target, got, tc.want)
			}
		})
	}
}

func TestNeedUpdateCPUSet(t *testing.T) {
	cases := []struct {
		name    string
		old     string
		new     string
		current string // recorded cpuStr in executor
		want    bool
	}{
		{"identical old new", "0-1", "0-1", "", false},
		{"empty new is no-op", "0-1", "", "", false},
		{"changed set", "0-1", "2-3", "", true},
		// old==new fast-path returns false even if recorded state differs
		{"recorded current differs but old==new fast-path", "0-1", "0-1", "2-3", false},
		{"recorded current matches", "0-1", "0-1", "0-1", false},
		{"whitespace trimmed", " 0-1 ", "0-1", "0-1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			me := newTestExecutor("test", 0, 0, 0, 0, 0, tc.current)
			if got := me.NeedUpdateCPUSet(tc.old, tc.new); got != tc.want {
				t.Errorf("NeedUpdateCPUSet(old=%q, new=%q, current=%q) = %v, want %v",
					tc.old, tc.new, tc.current, got, tc.want)
			}
		})
	}
}

func TestNeedUpdateCPUWeight(t *testing.T) {
	cases := []struct {
		name    string
		current uint32
		target  uint32
		want    bool
	}{
		{"same nonzero", 256, 256, false},
		{"changed", 256, 512, true},
		{"zero current", 0, 256, true},
		{"zero target nonzero current", 256, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			me := newTestExecutor("test", 0, 0, 0, tc.current, 0, "")
			if got := me.NeedUpdateCPUWeight(tc.target); got != tc.want {
				t.Errorf("NeedUpdateCPUWeight(current=%d, target=%d) = %v, want %v",
					tc.current, tc.target, got, tc.want)
			}
		})
	}
}

func TestNeedUpdateVCPUs(t *testing.T) {
	cases := []struct {
		name    string
		current uint32
		target  uint32
		want    bool
	}{
		{"zero target is sentinel no-op", 4, 0, false},
		// Hypervisor==nil in test → conservative always-true (except zero sentinel)
		{"same value (nil hypervisor conservative)", 4, 4, true},
		{"changed", 2, 4, true},
		{"zero current", 0, 2, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			me := newTestExecutor("test", 0, 0, tc.current, 0, 0, "")
			if got := me.NeedUpdateVCPUs(context.Background(), tc.target); got != tc.want {
				t.Errorf("NeedUpdateVCPUs(current=%d, target=%d) = %v, want %v",
					tc.current, tc.target, got, tc.want)
			}
		})
	}
}

func TestNeedUpdateCPUCap(t *testing.T) {
	cases := []struct {
		name    string
		current uint32
		target  uint32
		want    bool
	}{
		// Hypervisor==nil in test → conservative always-true
		{"same nonzero cap (nil hypervisor conservative)", 50, 50, true},
		{"changed cap", 50, 100, true},
		{"zero target always writes (clears stale)", 50, 0, true},
		{"zero current zero target (ambiguous)", 0, 0, true},
		{"zero current nonzero target", 0, 50, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			me := newTestExecutor("test", 0, 0, 0, 0, tc.current, "")
			if got := me.NeedUpdateCPUCap(context.Background(), tc.target); got != tc.want {
				t.Errorf("NeedUpdateCPUCap(current=%d, target=%d) = %v, want %v",
					tc.current, tc.target, got, tc.want)
			}
		})
	}
}

// TestReadResource verifies the snapshot fields extracted from records.
func TestReadResource(t *testing.T) {
	me := newTestExecutor("snap", 128, 256, 2, 100, 50, "0-1")
	res := me.ReadResource()
	if res == nil {
		t.Fatal("ReadResource returned nil")
	}
	if res.VCPU == nil || *res.VCPU != 2 {
		t.Errorf("VCPU = %v, want 2", res.VCPU)
	}
	if res.CPUWeight == nil || *res.CPUWeight != 100 {
		t.Errorf("CPUWeight = %v, want 100", res.CPUWeight)
	}
	if res.CPUCapacity == nil || *res.CPUCapacity != 50 {
		t.Errorf("CPUCapacity = %v, want 50", res.CPUCapacity)
	}
	if res.MemoryMaxMB == nil || *res.MemoryMaxMB != 128 {
		t.Errorf("MemoryMaxMB = %v, want 128", res.MemoryMaxMB)
	}
	if res.ClientCPUSet != "0-1" {
		t.Errorf("ClientCPUSet = %q, want %q", res.ClientCPUSet, "0-1")
	}
}

// TestReadResourceEmpty verifies nil-safe behavior for unpopulated records.
func TestReadResourceEmpty(t *testing.T) {
	me := newTestExecutor("empty", 0, 0, 0, 0, 0, "")
	res := me.ReadResource()
	if res == nil {
		t.Fatal("ReadResource returned nil for empty records")
	}
	if res.VCPU != nil {
		t.Errorf("VCPU should be nil for unpopulated, got %v", *res.VCPU)
	}
	if res.MemoryMaxMB != nil {
		t.Errorf("MemoryMaxMB should be nil for unpopulated, got %v", *res.MemoryMaxMB)
	}
	if res.ClientCPUSet != "" {
		t.Errorf("ClientCPUSet = %q, want empty", res.ClientCPUSet)
	}
}
