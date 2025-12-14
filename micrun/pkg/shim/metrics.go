// Package shim provides the implementation of the containerd shim v2 interface for micrun.
package shim

import (
	"context"
	"encoding/json"
	"fmt"
	er "micrun/errors"
	log "micrun/logger"
	cntr "micrun/pkg/micantainer"

	cgroupsv1 "github.com/containerd/cgroups/stats/v1"
	cgroupsv2 "github.com/containerd/cgroups/v2/stats"
	ptypes "github.com/containerd/containerd/protobuf/types"
	typeurl "github.com/containerd/typeurl/v2"
	"github.com/gogo/protobuf/proto"
)

// ContainerStats represents container statistics matching containerd's expected format.
type ContainerStats struct {
	CpuStats     CpuStats       `json:"cpu_stats"`
	MemoryStats  MemoryStats    `json:"memory_stats"`
	NetworkStats []NetworkStats `json:"network_stats"`
}

// CpuStats holds CPU usage and throttling data.
type CpuStats struct {
	CpuUsage       CpuUsage       `json:"cpu_usage"`
	ThrottlingData ThrottlingData `json:"throttling_data"`
}

// CpuUsage holds detailed CPU usage statistics.
type CpuUsage struct {
	TotalUsage        uint64   `json:"total_usage"`
	UsageInKernelmode uint64   `json:"usage_in_kernelmode"`
	UsageInUsermode   uint64   `json:"usage_in_usermode"`
	PercpuUsage       []uint64 `json:"percpu_usage"`
}

// ThrottlingData holds CPU throttling statistics.
type ThrottlingData struct {
	Periods          uint64 `json:"periods"`
	ThrottledPeriods uint64 `json:"throttled_periods"`
	ThrottledTime    uint64 `json:"throttled_time"`
}

// MemoryStats holds memory usage and cache statistics.
type MemoryStats struct {
	Usage MemoryEntry       `json:"usage"`
	Cache uint64            `json:"cache"`
	Stats map[string]uint64 `json:"stats"`
}

// MemoryEntry holds detailed memory usage data.
type MemoryEntry struct {
	Usage    uint64 `json:"usage"`
	MaxUsage uint64 `json:"max_usage"`
	Limit    uint64 `json:"limit"`
	Failcnt  uint64 `json:"failcnt"`
}

// NetworkStats holds network interface statistics.
type NetworkStats struct {
	Name      string `json:"name"`
	RxBytes   uint64 `json:"rx_bytes"`
	TxBytes   uint64 `json:"tx_bytes"`
	RxPackets uint64 `json:"rx_packets"`
	TxPackets uint64 `json:"tx_packets"`
	RxErrors  uint64 `json:"rx_errors"`
	TxErrors  uint64 `json:"tx_errors"`
	RxDropped uint64 `json:"rx_dropped"`
	TxDropped uint64 `json:"tx_dropped"`
}

// DummyStats generates dummy statistics data for testing purposes.
func (s *shimService) DummyStats() (*ptypes.Any, error) {
	// Using a map to avoid type registration issues with typeurl.
	dummyData := map[string]interface{}{
		"cpu_stats": map[string]interface{}{
			"cpu_usage": map[string]interface{}{
				"total_usage":         1500000000,                     // 1.5 seconds in nanoseconds.
				"usage_in_kernelmode": 300000000,                      // 300ms kernel time.
				"usage_in_usermode":   1200000000,                     // 1.2 seconds user time.
				"percpu_usage":        []uint64{750000000, 750000000}, // Per-core usage.
			},
			"throttling_data": map[string]interface{}{
				"periods":           1000,
				"throttled_periods": 50,
				"throttled_time":    100000000, // 100ms throttled time.
			},
		},
		"memory_stats": map[string]interface{}{
			"usage": map[string]interface{}{
				"usage":     104857600,  // 100MB current usage.
				"max_usage": 209715200,  // 200MB max usage.
				"limit":     1073741824, // 1GB limit.
				"failcnt":   0,
			},
			"cache": 52428800, // 50MB cache.
			"stats": map[string]uint64{
				"active_anon":   52428800,
				"inactive_anon": 0,
				"active_file":   52428800,
				"inactive_file": 0,
				"pgfault":       1000,
				"pgmajfault":    5,
			},
		},
		"network_stats": []map[string]interface{}{
			{
				"name":       "eth0",
				"rx_bytes":   1024000, // 1MB received.
				"tx_bytes":   512000,  // 512KB transmitted.
				"rx_packets": 1000,
				"tx_packets": 500,
				"rx_errors":  0,
				"tx_errors":  0,
				"rx_dropped": 0,
				"tx_dropped": 0,
			},
		},
	}

	jsonData, err := json.Marshal(dummyData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal dummy stats to JSON: %w", err)
	}

	// Create a protobuf Any with the JSON data.
	return &ptypes.Any{
		TypeUrl: "type.googleapis.com/google.protobuf.Struct",
		Value:   jsonData,
	}, nil
}

// marshalMetrics collects container stats and marshals them into a protobuf Any type.
func marshalMetrics(ctx context.Context, s *shimService, cid string) (*ptypes.Any, error) {
	if s.sandbox == nil {
		log.Debugf("Sandbox is nil, cannot get stats for container %s", cid)
		return nil, er.SandboxNotFound
	}
	stats, err := s.sandbox.StatsContainer(ctx, cid)
	if err != nil {
		return nil, err
	}

	// Force CRI-compatible metrics (cgroups v1) to avoid decode errors in kubelet/k3s.
	isCgroupV1, err := cgroupV1()
	if err != nil {
		log.Debugf("Failed to determine cgroup version: %v", err)
		isCgroupV1 = true
	}

	var metrics proto.Message

	var (
		cpuUsageUsec   uint64
		memUsageBytes  uint64
		memLimitBytes  uint64
		memFailcnt     uint64
		memMaxBytes    uint64
		resourceExists bool
	)

	if stats.ResourceStats != nil {
		resourceExists = true
		cpuUsageUsec = stats.ResourceStats.CPUStats.TotalUsage
		memUsageBytes = stats.ResourceStats.MemoryStats.Usage.Usage
		memLimitBytes = stats.ResourceStats.MemoryStats.Usage.Limit
		memFailcnt = stats.ResourceStats.MemoryStats.Usage.Failcnt
		memMaxBytes = stats.ResourceStats.MemoryStats.Usage.MaxEver
	}

	if isCgroupV1 {
		metrics = statsToMetricsV1(&stats)
	} else {
		metrics = statsToMetricsV2(&stats)
	}

	// Ensure metrics is not nil before marshaling
	if metrics == nil {
		return nil, fmt.Errorf("metrics is nil for container %s", cid)
	}

	typeAny, err := typeurl.MarshalAny(metrics)
	if err != nil {
		return nil, err
	}

	log.Debugf(
		"marshalMetrics: container=%s cgroup_v1=%t resources=%t cpu_usage_usec=%d memory_usage_bytes=%d memory_limit_bytes=%d memory_failcnt=%d memory_max_bytes=%d type=%s",
		cid,
		isCgroupV1,
		resourceExists,
		cpuUsageUsec,
		memUsageBytes,
		memLimitBytes,
		memFailcnt,
		memMaxBytes,
		typeAny.GetTypeUrl(),
	)

	return &ptypes.Any{
		TypeUrl: typeAny.GetTypeUrl(),
		Value:   typeAny.GetValue(),
	}, nil
}

// EmptyMetricsV1 returns empty metrics for when stats collection fails.
func EmptyMetricsV1() *ptypes.Any {
	m := statsToMetricsV1(nil)
	typeAny, err := typeurl.MarshalAny(m)
	if err != nil {
		return &ptypes.Any{}
	}
	return &ptypes.Any{
		TypeUrl: typeAny.GetTypeUrl(),
		Value:   typeAny.GetValue(),
	}
}

// statsToMetricsV1 converts micantainer stats to cgroups v1 metrics format.
func statsToMetricsV1(stats *cntr.ContainerStats) *cgroupsv1.Metrics {
	m := &cgroupsv1.Metrics{}

	if stats == nil || stats.ResourceStats == nil {
		return m
	}

	cpuStats := stats.ResourceStats.CPUStats
	memStats := stats.ResourceStats.MemoryStats

	m.CPU = &cgroupsv1.CPUStat{
		Usage: &cgroupsv1.CPUUsage{
			Total: cpuStats.TotalUsage * 1000, // convert µs to ns for CRI consumers.
		},
		Throttling: &cgroupsv1.Throttle{
			Periods: cpuStats.NrPeriods,
		},
	}

	m.Memory = &cgroupsv1.MemoryStat{
		Cache: memStats.Cache,
		Usage: &cgroupsv1.MemoryEntry{
			Usage:   memStats.Usage.Usage,
			Limit:   memStats.Usage.Limit,
			Max:     memStats.Usage.MaxEver,
			Failcnt: memStats.Usage.Failcnt,
		},
		TotalRSS:        memStats.Stats["total_rss"],
		TotalCache:      memStats.Stats["total_cache"],
		TotalPgFault:    memStats.Stats["pgfault"],
		TotalPgMajFault: memStats.Stats["pgmajfault"],
	}

	return m
}

// statsToMetricsV2 converts micantainer stats to cgroups v2 metrics format.
func statsToMetricsV2(stats *cntr.ContainerStats) *cgroupsv2.Metrics {
	m := &cgroupsv2.Metrics{
		Pids: &cgroupsv2.PidsStat{
			Current: uint64(shimPid),
			Limit:   uint64(shimPid),
		},
	}

	if stats != nil && stats.ResourceStats != nil {
		m.CPU = setMetricsCPUStats(&stats.ResourceStats.CPUStats)
		m.Memory = setMetricsMemStats(&stats.ResourceStats.MemoryStats)
	}

	return m
}

// setMetricsCPUStats populates the cgroups v2 CPUStat from micantainer CPUStats.
func setMetricsCPUStats(cs *cntr.CPUStats) *cgroupsv2.CPUStat {
	cpuStats := &cgroupsv2.CPUStat{
		UsageUsec:     cs.TotalUsage,
		UserUsec:      cs.TotalUsage,
		SystemUsec:    0,
		NrPeriods:     cs.NrPeriods,
		ThrottledUsec: 0,
	}
	return cpuStats
}

// setMetricsMemStats populates the cgroups v2 MemoryStat from micantainer MemoryStats.
func setMetricsMemStats(ms *cntr.MemoryStats) *cgroupsv2.MemoryStat {
	memStats := &cgroupsv2.MemoryStat{
		Usage:      ms.Usage.Usage,
		UsageLimit: ms.Usage.Limit,
	}
	return memStats
}
