package shim

import (
	cgroupsv1 "github.com/containerd/cgroups/stats/v1"
	cgroupsv2 "github.com/containerd/cgroups/v2/stats"

	cntr "micrun/internal/domain/container"
)

// statsToMetricsV1 converts container domain stats to cgroups v1 metrics format.
func statsToMetricsV1(stats *cntr.ContainerStats) *cgroupsv1.Metrics {
	m := &cgroupsv1.Metrics{}

	if stats == nil || stats.ResourceStats == nil {
		return m
	}

	cpuStats := stats.ResourceStats.CPUStats
	memStats := stats.ResourceStats.MemoryStats

	m.CPU = &cgroupsv1.CPUStat{
		Usage: &cgroupsv1.CPUUsage{
			Total: cpuStats.TotalUsage * 1000,
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

// statsToMetricsV2 converts container domain stats to cgroups v2 metrics format.
func statsToMetricsV2(stats *cntr.ContainerStats, pid uint32) *cgroupsv2.Metrics {
	m := &cgroupsv2.Metrics{
		Pids: &cgroupsv2.PidsStat{
			Current: uint64(pid),
			Limit:   uint64(pid),
		},
	}

	if stats != nil && stats.ResourceStats != nil {
		m.CPU = metricsCPUStatsV2(&stats.ResourceStats.CPUStats)
		m.Memory = metricsMemStatsV2(&stats.ResourceStats.MemoryStats)
	}

	return m
}

// metricsCPUStatsV2 populates the cgroups v2 CPUStat from container CPUStats.
func metricsCPUStatsV2(cs *cntr.CPUStats) *cgroupsv2.CPUStat {
	return &cgroupsv2.CPUStat{
		UsageUsec:     cs.TotalUsage,
		UserUsec:      cs.TotalUsage,
		SystemUsec:    0,
		NrPeriods:     cs.NrPeriods,
		ThrottledUsec: 0,
	}
}

// metricsMemStatsV2 populates the cgroups v2 MemoryStat from container MemoryStats.
func metricsMemStatsV2(ms *cntr.MemoryStats) *cgroupsv2.MemoryStat {
	return &cgroupsv2.MemoryStat{
		Usage:      ms.Usage.Usage,
		UsageLimit: ms.Usage.Limit,
	}
}
