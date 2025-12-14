package pedestal

import (
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func TestCPUCapacityWithCpuset(t *testing.T) {
	tests := []struct {
		name     string
		quota    int64
		period   uint64
		cpuset   string
		expected uint32
	}{
		{
			name:     "cpuset限制更严格",
			quota:    200000,
			period:   100000,
			cpuset:   "0",
			expected: 100,
		},
		{
			name:     "quota限制更严格",
			quota:    50000,
			period:   100000,
			cpuset:   "0-3",
			expected: 50,
		},
		{
			name:     "无quota只有cpuset",
			quota:    0,
			period:   100000,
			cpuset:   "0-1",
			expected: 200,
		},
		{
			name:     "无cpuset只有quota",
			quota:    150000,
			period:   100000,
			cpuset:   "",
			expected: 150,
		},
		{
			name:     "quota无效负数",
			quota:    -1,
			period:   100000,
			cpuset:   "0-2",
			expected: 300,
		},
		{
			name:     "cpuset空quota有效",
			quota:    80000,
			period:   100000,
			cpuset:   "",
			expected: 80,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := &specs.Spec{
				Linux: &specs.Linux{
					Resources: &specs.LinuxResources{
						CPU: &specs.LinuxCPU{},
					},
				},
			}
			if tt.quota != 0 {
				quotaVal := tt.quota
				spec.Linux.Resources.CPU.Quota = &quotaVal
			}
			if tt.period != 0 {
				periodVal := tt.period
				spec.Linux.Resources.CPU.Period = &periodVal
			}
			if tt.cpuset != "" {
				spec.Linux.Resources.CPU.Cpus = tt.cpuset
			}

			res := linuxResourceToEssential(spec, false)
			if *res.CpuCpacity != tt.expected {
				t.Errorf("CpuCpacity = %d, want %d", *res.CpuCpacity, tt.expected)
			}
		})
	}
}