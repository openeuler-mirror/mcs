package pedestal

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/shirou/gopsutil/v3/mem"

	log "micrun/internal/support/logger"
)

func xinfo(ctx context.Context) (*XlInfo, error) {
	cmd := newXLContext(ctx, info)

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to run xl info: %w", err)
	}

	return parseXlInfo(out.String())
}

type xlInfoSetter func(info *XlInfo, value string) error

var xlInfoFields = map[string]xlInfoSetter{
	"host":                  func(i *XlInfo, v string) error { i.host = v; return nil },
	"machine":               func(i *XlInfo, v string) error { i.machine = v; return nil },
	"nr_cpus":               parseUint32Field("nr_cpus", func(i *XlInfo, v uint32) { i.nrCpus = v }),
	"total_memory":          parseUint32Field("total_memory", func(i *XlInfo, v uint32) { i.totalMemoryMB = v }),
	"free_memory":           parseUint32Field("free_memory", func(i *XlInfo, v uint32) { i.freeMemoryMB = v }),
	"xen_major":             func(i *XlInfo, v string) error { i.xenMajor = v; return nil },
	"xen_minor":             func(i *XlInfo, v string) error { i.xenMinor = v; return nil },
	"xen_extra":             func(i *XlInfo, v string) error { i.xenExtra = v; return nil },
	"max_cpu_id":            parseUint32Field("max_cpu_id", func(i *XlInfo, v uint32) { i.maxCpuId = v }),
	"cores_per_socket":      parseUint32Field("cores_per_socket", func(i *XlInfo, v uint32) { i.coresPerSocket = v }),
	"threads_per_core":      parseUint32Field("threads_per_core", func(i *XlInfo, v uint32) { i.threadsPerCore = v }),
	"cpu_mhz":               parseFloatField("cpu_mhz", func(i *XlInfo, v float64) { i.cpuMhz = v }),
	"free_cpus":             parseUint32Field("free_cpus", func(i *XlInfo, v uint32) { i.freeCpus = v }),
	"xen_caps":              func(i *XlInfo, v string) error { i.xenCaps = v; return nil },
	"xen_scheduler":         func(i *XlInfo, v string) error { i.xenScheduler = v; return nil },
	"xen_pagesize":          parseUint32Field("xen_pagesize", func(i *XlInfo, v uint32) { i.xenPagesize = v }),
	"virt_caps":             func(i *XlInfo, v string) error { i.virtCaps = v; return nil },
	"outstanding_claims":    parseUint64Field("outstanding_claims", func(i *XlInfo, v uint64) { i.outstandingClaims = v }),
	"sharing_freed_memory":  parseUint64Field("sharing_freed_memory", func(i *XlInfo, v uint64) { i.sharingFreedMemory = v }),
	"sharing_used_memory":   parseUint64Field("sharing_used_memory", func(i *XlInfo, v uint64) { i.sharingUsedMemory = v }),
	"platform_params":       func(i *XlInfo, v string) error { i.platformParams = v; return nil },
	"xen_commandline":       func(i *XlInfo, v string) error { i.xenCommandline = v; return nil },
	"arm_sve_vector_length": parseUint32Field("arm_sve_vector_length", func(i *XlInfo, v uint32) { i.armSVEVectorLength = v }),
}

func parseXlInfo(output string) (*XlInfo, error) {
	info := &XlInfo{}
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if setter, ok := xlInfoFields[key]; ok {
			if err := setter(info, value); err != nil {
				return nil, err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading xl info output: %w", err)
	}

	// Assemble the Xen version string from its components in a fixed order,
	// independent of the field order in `xl info` output.
	if info.xenMajor != "" {
		info.xlver = info.xenMajor
		if info.xenMinor != "" {
			info.xlver += "." + info.xenMinor
		}
		info.xlver += info.xenExtra
	}

	return info, nil
}

func parseUint32Field(name string, assign func(*XlInfo, uint32)) xlInfoSetter {
	return func(info *XlInfo, value string) error {
		v, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", name, err)
		}
		assign(info, uint32(v))
		return nil
	}
}

func parseUint64Field(name string, assign func(*XlInfo, uint64)) xlInfoSetter {
	return func(info *XlInfo, value string) error {
		v, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", name, err)
		}
		assign(info, v)
		return nil
	}
}

func parseFloatField(name string, assign func(*XlInfo, float64)) xlInfoSetter {
	return func(info *XlInfo, value string) error {
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", name, err)
		}
		assign(info, v)
		return nil
	}
}

func (xi *XlInfo) nodePhysicalCPUNum() uint32 {
	return xi.nrCpus
}

// MemoryMB returns the amount of free and total memory in MB.
func MemoryMB(ctx context.Context) (free, total uint32) {
	v, err := mem.VirtualMemory()
	if err == nil && v != nil {
		free = uint32(v.Free >> 20)
		total = uint32(v.Total >> 20)
	}

	i, err := xinfo(ctx)
	if err != nil {
		log.Debugf("failed to get machine info: %v", err)
		return free, total
	}
	xlFree, xlTotal, ok := xlInfoMemoryMB(i)
	if !ok {
		log.Warnf("MemoryMB: xl info reported no usable total memory, falling back to host memory (%d MiB)", total)
		return free, total
	}
	return xlFree, xlTotal
}

// xlInfoMemoryMB reports the memory `xl info` observed, and whether it is
// usable at all. A zero total means `xl info` ran but reported no
// total_memory line (trimmed xl build, renamed field, unexpected output
// shape) — a parse anomaly, not a host with no memory. Callers must fall back
// to the gopsutil reading instead of propagating the zero: it would clamp
// MemHighThreshold to the 2 MiB floor, after which every configured container
// memory value fails memoryWithinHostBounds and the runtime config silently
// degrades to the floor. Same policy as MaxCPUNum's zero guard.
func xlInfoMemoryMB(i *XlInfo) (free, total uint32, ok bool) {
	if i == nil || i.totalMemoryMB == 0 {
		return 0, 0, false
	}
	return i.freeMemoryMB, i.totalMemoryMB, true
}

var (
	maxCPUNum    uint32
	maxCPUNumSet bool
	maxCPUNumMu  sync.Mutex
)

// MaxCPUNum returns the number of physical CPUs as reported by xl info,
// cached after the first successful query. The (possibly slow) xl info
// subprocess runs OUTSIDE the lock so concurrent callers are not serialized
// behind it; on success the result is published under the lock (first result
// wins — they are equivalent). A failed or zero query is never cached, so
// the next call retries instead of poisoning CPU-capacity decisions.
func MaxCPUNum(ctx context.Context) uint32 {
	maxCPUNumMu.Lock()
	if maxCPUNumSet {
		n := maxCPUNum
		maxCPUNumMu.Unlock()
		return n
	}
	maxCPUNumMu.Unlock()

	i, err := xinfo(ctx)
	if err != nil {
		log.Debugf("failed to get machine info: %v", err)
		return uint32(runtime.NumCPU())
	}
	n := i.nodePhysicalCPUNum()
	if n == 0 {
		// A zero value indicates a parse anomaly; do not cache it, so a
		// subsequent call can retry instead of poisoning every CPU-capacity
		// decision with 0.
		log.Warnf("MaxCPUNum: xl info reported 0 physical CPUs, not caching")
		return uint32(runtime.NumCPU())
	}
	maxCPUNumMu.Lock()
	if !maxCPUNumSet {
		maxCPUNum = n
		maxCPUNumSet = true
	}
	maxCPUNumMu.Unlock()
	log.Debugf("MaxCPUNum initialized to: %d", n)
	return n
}

func MemLowThreshold() uint32 {
	return 2
}

func MemHighThreshold(ctx context.Context) uint32 {
	xi, err := xinfo(ctx)
	var xlTotal uint32
	var usable bool
	if err == nil {
		_, xlTotal, usable = xlInfoMemoryMB(xi)
	}
	// A zero total is treated exactly like a failed query (see
	// xlInfoMemoryMB): fall back to gopsutil instead of returning 0, because
	// a zero high threshold makes host_profile clamp high=low=2 and every
	// container memory request is then rejected as exceeding the host limit.
	if !usable {
		log.Debugf("failed to get usable machine memory info: %v", err)
		if v, err := mem.VirtualMemory(); err == nil && v != nil {
			return uint32(v.Total >> 20)
		}
		return 0
	}
	maxMem := xlTotal
	if maxMem < MemLowThreshold() {
		maxMem = MemLowThreshold() + 1
	}
	return maxMem
}
