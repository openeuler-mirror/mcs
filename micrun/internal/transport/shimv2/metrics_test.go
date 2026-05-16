package shim

import (
	"testing"

	cntr "micrun/internal/domain/container"
)

func TestStatsToMetricsV1(t *testing.T) {
	stats := &cntr.ContainerStats{
		ResourceStats: &cntr.ResourceStats{
			CPUStats: cntr.CPUStats{
				TotalUsage: 321,
				NrPeriods:  9,
			},
			MemoryStats: cntr.MemoryStats{
				Cache: 7,
				Usage: cntr.MemoryEntry{
					Usage:   11,
					Limit:   22,
					MaxEver: 33,
					Failcnt: 44,
				},
				Stats: map[string]uint64{
					"total_rss":   55,
					"total_cache": 66,
					"pgfault":     77,
					"pgmajfault":  88,
				},
			},
		},
	}

	metrics := statsToMetricsV1(stats)
	if metrics.CPU == nil || metrics.Memory == nil {
		t.Fatal("expected CPU and Memory metrics to be populated")
	}
	if metrics.CPU.Usage.Total != 321000 {
		t.Fatalf("CPU total = %d, want 321000", metrics.CPU.Usage.Total)
	}
	if metrics.CPU.Throttling.Periods != 9 {
		t.Fatalf("CPU periods = %d, want 9", metrics.CPU.Throttling.Periods)
	}
	if metrics.Memory.Usage.Usage != 11 || metrics.Memory.Usage.Limit != 22 || metrics.Memory.Usage.Max != 33 || metrics.Memory.Usage.Failcnt != 44 {
		t.Fatalf("unexpected memory usage payload: %+v", metrics.Memory.Usage)
	}
	if metrics.Memory.TotalRSS != 55 || metrics.Memory.TotalCache != 66 || metrics.Memory.TotalPgFault != 77 || metrics.Memory.TotalPgMajFault != 88 {
		t.Fatalf("unexpected memory totals: %+v", metrics.Memory)
	}
}

func TestStatsToMetricsV2(t *testing.T) {
	stats := &cntr.ContainerStats{
		ResourceStats: &cntr.ResourceStats{
			CPUStats: cntr.CPUStats{
				TotalUsage: 654,
				NrPeriods:  12,
			},
			MemoryStats: cntr.MemoryStats{
				Usage: cntr.MemoryEntry{
					Usage: 99,
					Limit: 199,
				},
			},
		},
	}

	metrics := statsToMetricsV2(stats, 4242)
	if metrics.CPU == nil || metrics.Memory == nil || metrics.Pids == nil {
		t.Fatal("expected CPU, Memory and Pids metrics to be populated")
	}
	if metrics.CPU.UsageUsec != 654 || metrics.CPU.UserUsec != 654 || metrics.CPU.NrPeriods != 12 {
		t.Fatalf("unexpected CPU v2 metrics: %+v", metrics.CPU)
	}
	if metrics.Memory.Usage != 99 || metrics.Memory.UsageLimit != 199 {
		t.Fatalf("unexpected Memory v2 metrics: %+v", metrics.Memory)
	}
	if metrics.Pids.Current != 4242 || metrics.Pids.Limit != 4242 {
		t.Fatalf("unexpected Pids metrics: %+v", metrics.Pids)
	}
}

func TestStatsToMetricsHandlesNilStats(t *testing.T) {
	if metrics := statsToMetricsV1(nil); metrics == nil {
		t.Fatal("expected empty v1 metrics, got nil")
	}
	if metrics := statsToMetricsV2(nil, 4242); metrics == nil || metrics.Pids == nil {
		t.Fatal("expected v2 metrics with pid stats, got nil")
	}
}
