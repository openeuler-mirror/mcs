package pedestal

import (
	"testing"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// TestCPUResourceMapping 测试 CPU 资源映射逻辑
func TestCPUResourceMapping(t *testing.T) {
	tests := []struct {
		name           string
		spec           *specs.Spec
		convertShares  bool
		expectedVcpu   uint32
		expectedCap    uint32
		expectedWeight uint32
		expectedCpuset string
	}{
		{
			name: "默认配置 - 无限制",
			spec: &specs.Spec{
				Linux: &specs.Linux{
					Resources: &specs.LinuxResources{
						CPU: &specs.LinuxCPU{},
					},
				},
			},
			convertShares:  true,
			expectedVcpu:   1,   // 默认 VCPU = 1
			expectedCap:    0,   // 无 quota/period，容量为 0
			expectedWeight: 256, // Xen 默认权重
			expectedCpuset: "",
		},
		{
			name: "CPU Shares 映射",
			spec: &specs.Spec{
				Linux: &specs.Linux{
					Resources: &specs.LinuxResources{
						CPU: &specs.LinuxCPU{
							Shares: uint64Ptr(1024), // cgroup 默认值
						},
					},
				},
			},
			convertShares:  true,
			expectedVcpu:   1,
			expectedCap:    0,
			expectedWeight: 256, // 1024 / 4 = 256
			expectedCpuset: "",
		},
		{
			name: "CPU Quota/Period 映射 - 0.5核",
			spec: &specs.Spec{
				Linux: &specs.Linux{
					Resources: &specs.LinuxResources{
						CPU: &specs.LinuxCPU{
							Quota:  int64Ptr(50000),   // 50ms
							Period: uint64Ptr(100000), // 100ms
						},
					},
				},
			},
			convertShares:  true,
			expectedVcpu:   1,
			expectedCap:    50, // (50000 * 100) / 100000 = 50%
			expectedWeight: 256,
			expectedCpuset: "",
		},
		{
			name: "CPU Quota/Period 映射 - 1.5核",
			spec: &specs.Spec{
				Linux: &specs.Linux{
					Resources: &specs.LinuxResources{
						CPU: &specs.LinuxCPU{
							Quota:  int64Ptr(150000),  // 150ms
							Period: uint64Ptr(100000), // 100ms
						},
					},
				},
			},
			convertShares:  true,
			expectedVcpu:   1,
			expectedCap:    150, // (150000 * 100) / 100000 = 150%
			expectedWeight: 256,
			expectedCpuset: "",
		},
		{
			name: "CPU Set 映射",
			spec: &specs.Spec{
				Linux: &specs.Linux{
					Resources: &specs.LinuxResources{
						CPU: &specs.LinuxCPU{
							Cpus: "0-3",
						},
					},
				},
			},
			convertShares:  true,
			expectedVcpu:   4,   // cpuset 大小 = 4
			expectedCap:    400, // 无 quota/period，容量 = cpuset_size * 100%
			expectedWeight: 256,
			expectedCpuset: "0-3",
		},
		{
			name: "CPU Set + Quota/Period 映射 - cpuset 限制更严格",
			spec: &specs.Spec{
				Linux: &specs.Linux{
					Resources: &specs.LinuxResources{
						CPU: &specs.LinuxCPU{
							Cpus:   "0",               // 1个核心
							Quota:  int64Ptr(200000),  // 200ms
							Period: uint64Ptr(100000), // 100ms
						},
					},
				},
			},
			convertShares:  true,
			expectedVcpu:   1,
			expectedCap:    100, // min(200%, 100%) = 100%
			expectedWeight: 256,
			expectedCpuset: "0",
		},
		{
			name: "CPU Set + Quota/Period 映射 - quota 限制更严格",
			spec: &specs.Spec{
				Linux: &specs.Linux{
					Resources: &specs.LinuxResources{
						CPU: &specs.LinuxCPU{
							Cpus: "0-3",
							// 4个核心
							Quota:  int64Ptr(50000),   // 50ms
							Period: uint64Ptr(100000), // 100ms
						},
					},
				},
			},
			convertShares:  true,
			expectedVcpu:   4,
			expectedCap:    50, // min(50%, 400%) = 50%
			expectedWeight: 256,
			expectedCpuset: "0-3",
		},
		{
			name: "完整 CPU 配置",
			spec: &specs.Spec{
				Linux: &specs.Linux{
					Resources: &specs.LinuxResources{
						CPU: &specs.LinuxCPU{
							Shares: uint64Ptr(2048), // 2倍默认权重
							Cpus:   "0,2,4",
							Quota:  int64Ptr(75000),   // 75ms
							Period: uint64Ptr(100000), // 100ms
						},
					},
				},
			},
			convertShares:  true,
			expectedVcpu:   3,   // cpuset 大小 = 3
			expectedCap:    75,  // (75000 * 100) / 100000 = 75%
			expectedWeight: 512, // 2048 / 4 = 512
			expectedCpuset: "0,2,4",
		},
		{
			name: "Baremetal 模式 - 不转换 shares",
			spec: &specs.Spec{
				Linux: &specs.Linux{
					Resources: &specs.LinuxResources{
						CPU: &specs.LinuxCPU{
							Shares: uint64Ptr(1024),
						},
					},
				},
			},
			convertShares:  false,
			expectedVcpu:   1,
			expectedCap:    0,
			expectedWeight: 1024, // 不转换，直接使用 shares 值
			expectedCpuset: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := linuxResourceToEssential(tt.spec, tt.convertShares)

			// 验证 VCPU 数量
			if res.Vcpu == nil {
				t.Errorf("Vcpu is nil")
			} else if *res.Vcpu != tt.expectedVcpu {
				t.Errorf("Vcpu = %d, want %d", *res.Vcpu, tt.expectedVcpu)
			}

			// 验证 CPU 容量
			if res.CpuCpacity == nil {
				if tt.expectedCap != 0 {
					t.Errorf("CpuCpacity is nil, want %d", tt.expectedCap)
				}
			} else if *res.CpuCpacity != tt.expectedCap {
				t.Errorf("CpuCpacity = %d, want %d", *res.CpuCpacity, tt.expectedCap)
			}

			// 验证 CPU 权重
			if tt.expectedWeight == 0 {
				if res.CPUWeight != nil {
					t.Errorf("CPUWeight should be nil, got %v", *res.CPUWeight)
				}
			} else {
				if res.CPUWeight == nil {
					t.Errorf("CPUWeight is nil, want %d", tt.expectedWeight)
				} else if *res.CPUWeight != tt.expectedWeight {
					t.Errorf("CPUWeight = %d, want %d", *res.CPUWeight, tt.expectedWeight)
				}
			}

			// 验证 CPU Set
			if res.ClientCpuSet != tt.expectedCpuset {
				t.Errorf("ClientCpuSet = %q, want %q", res.ClientCpuSet, tt.expectedCpuset)
			}
		})
	}
}

// TestMemoryResourceMapping 测试内存资源映射逻辑
func TestMemoryResourceMapping(t *testing.T) {
	tests := []struct {
		name     string
		spec     *specs.Spec
		expected uint32
	}{
		{
			name: "无内存限制",
			spec: &specs.Spec{
				Linux: &specs.Linux{
					Resources: &specs.LinuxResources{
						Memory: &specs.LinuxMemory{},
					},
				},
			},
			expected: 16, // defs.DefaultMinMemMB
		},
		{
			name: "内存限制 512MB",
			spec: &specs.Spec{
				Linux: &specs.Linux{
					Resources: &specs.LinuxResources{
						Memory: &specs.LinuxMemory{
							Limit: int64Ptr(512 * 1024 * 1024), // 512MB
						},
					},
				},
			},
			expected: 512,
		},
		{
			name: "内存限制 1GB",
			spec: &specs.Spec{
				Linux: &specs.Linux{
					Resources: &specs.LinuxResources{
						Memory: &specs.LinuxMemory{
							Limit: int64Ptr(1024 * 1024 * 1024), // 1GB
						},
					},
				},
			},
			expected: 1024,
		},
		{
			name: "内存限制 + 预留内存",
			spec: &specs.Spec{
				Linux: &specs.Linux{
					Resources: &specs.LinuxResources{
						Memory: &specs.LinuxMemory{
							Limit:       int64Ptr(1024 * 1024 * 1024), // 1GB
							Reservation: int64Ptr(512 * 1024 * 1024),  // 512MB
						},
					},
				},
			},
			expected: 1024, // 只映射 limit，reservation 在别处处理
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := linuxResourceToEssential(tt.spec, true)

			// 验证内存限制
			if tt.expected == 0 {
				// 注意：InitResource() 默认设置 MemoryMaxMB = defs.DefaultMinMemMB (16)
				// 所以当没有内存限制时，MemoryMaxMB 应该是 16 而不是 0
				if res.MemoryMaxMB == nil || *res.MemoryMaxMB != 16 {
					t.Errorf("MemoryMaxMB should be 16 (DefaultMinMemMB), got %v", res.MemoryMaxMB)
				}
			} else {
				if res.MemoryMaxMB == nil {
					t.Errorf("MemoryMaxMB is nil, want %d", tt.expected)
				} else if *res.MemoryMaxMB != tt.expected {
					t.Errorf("MemoryMaxMB = %d, want %d", *res.MemoryMaxMB, tt.expected)
				}
			}
		})
	}
}

// TestShareToWeight 测试 CPU shares 到 weight 的转换
func TestShareToWeight(t *testing.T) {
	tests := []struct {
		name   string
		shares uint64
		want   uint32
	}{
		{
			name:   "零 shares 使用默认值",
			shares: 0,
			want:   DefaultXenWeight, // 256
		},
		{
			name:   "cgroup 默认值",
			shares: 1024,
			want:   256, // 1024 / 4 = 256
		},
		{
			name:   "2倍默认值",
			shares: 2048,
			want:   512, // 2048 / 4 = 512
		},
		{
			name:   "最小值边界",
			shares: 1,
			want:   1, // 1 / 4 = 0，但最小值为 1
		},
		{
			name:   "最大值边界",
			shares: 65535*uint64(ShareWeightRatio) + 64,
			want:   65535, // 超过最大值，截断到 65535
		},
		{
			name:   "中等值",
			shares: 5000,
			want:   1250, // 5000 / 4 = 1250
		},
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

// TestResourceMappingPrinciples 测试资源映射原则
func TestResourceMappingPrinciples(t *testing.T) {
	// 测试 1: CPU 容量为 0 表示不限制
	t.Run("CPU容量为0表示不限制", func(t *testing.T) {
		spec := &specs.Spec{
			Linux: &specs.Linux{
				Resources: &specs.LinuxResources{
					CPU: &specs.LinuxCPU{
						Quota:  int64Ptr(-1), // -1 表示不限制
						Period: uint64Ptr(100000),
					},
				},
			},
		}

		res := linuxResourceToEssential(spec, true)
		if res.CpuCpacity == nil || *res.CpuCpacity != 0 {
			t.Errorf("CPU容量应该为0表示不限制，got %v", res.CpuCpacity)
		}
	})

	// 测试 2: 只有 cpuset 没有 quota/period
	t.Run("只有cpuset没有quota/period", func(t *testing.T) {
		spec := &specs.Spec{
			Linux: &specs.Linux{
				Resources: &specs.LinuxResources{
					CPU: &specs.LinuxCPU{
						Cpus: "0-1",
					},
				},
			},
		}

		res := linuxResourceToEssential(spec, true)
		if res.Vcpu == nil || *res.Vcpu != 2 {
			t.Errorf("VCPU数量应该为2，got %v", res.Vcpu)
		}
		if res.CpuCpacity == nil || *res.CpuCpacity != 200 {
			t.Errorf("CPU容量应该为200%%，got %v", res.CpuCpacity)
		}
	})

	// 测试 3: 无效的 cpuset 处理
	t.Run("无效cpuset处理", func(t *testing.T) {
		spec := &specs.Spec{
			Linux: &specs.Linux{
				Resources: &specs.LinuxResources{
					CPU: &specs.LinuxCPU{
						Cpus: "invalid-cpuset",
					},
				},
			},
		}

		res := linuxResourceToEssential(spec, true)
		if res.ClientCpuSet != "" {
			t.Errorf("无效cpuset应该被清空，got %q", res.ClientCpuSet)
		}
		if res.Vcpu == nil || *res.Vcpu != 1 {
			t.Errorf("无效cpuset时VCPU应该为默认值1，got %v", res.Vcpu)
		}
	})
}

// 辅助函数
func uint64Ptr(v uint64) *uint64 {
	return &v
}

func int64Ptr(v int64) *int64 {
	return &v
}
