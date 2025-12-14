package micantainer

// ResourceStats holds CPU and memory statistics.
type ResourceStats struct {
	CPUStats    CPUStats    `json:"cpu_stats"`
	MemoryStats MemoryStats `json:"memory_stats"`
}

// CPUStats holds CPU usage statistics.
type CPUStats struct {
	// TotalUsage is the total physical CPU time spent on the current container.
	// In cgroup metrics, CPUStat includes UserUsec and SystemUsec, but it's
	// unnecessary for an RTOS to calculate them separately.
	TotalUsage uint64 `json:"total_usage,omitempty"`
	// NrPeriods is the number of schedule cycles after the client is created,
	// if the pedestal supports it.
	NrPeriods uint64 `json:"nr_periods,omitempty"`
}

// MemoryStats holds memory usage statistics.
type MemoryStats struct {
	Cache uint64            `json:"cache"`
	Usage MemoryEntry       `json:"usage"`
	Stats map[string]uint64 `json:"stats"`
}

// MemoryEntry holds detailed memory usage data.
type MemoryEntry struct {
	Failcnt uint64 `json:"failcnt,omitempty"`
	Limit   uint64 `json:"limit,omitempty"`
	// MaxEver is the maximum memory usage recorded. In static allocation, MaxEver equals Limit.
	MaxEver uint64 `json:"max_ever,omitempty"`
	Usage   uint64 `json:"usage,omitempty"`
}

type NetworkStats struct {
	Name      string `json:"name,omitempty"`
	RxBytes   uint64 `json:"rx_bytes,omitempty"`
	RxPackets uint64 `json:"rx_packets,omitempty"`
	RxErrors  uint64 `json:"rx_errors,omitempty"`
	RxDropped uint64 `json:"rx_dropped,omitempty"`
	TxBytes   uint64 `json:"tx_bytes,omitempty"`
	TxPackets uint64 `json:"tx_packets,omitempty"`
	TxErrors  uint64 `json:"tx_errors,omitempty"`
	TxDropped uint64 `json:"tx_dropped,omitempty"`
}

// ContainerStats holds statistics for a container.
type ContainerStats struct {
	ResourceStats *ResourceStats
	NetworkStats  []*NetworkStats
}
