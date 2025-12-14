// Package pedestal provides functionality for interacting with different pedestal hypervisors.
// TODO: re-orgnize the package for better construction
package pedestal

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/shirou/gopsutil/v3/mem"

	er "micrun/errors"
	log "micrun/logger"
	"micrun/pkg/cpuset"
)

// CPU 资源映射常量
// 映射关系：Container CPU Share (1024:256) -> RTOS Client CPU Weight
// 转换比例：1024 (cgroup默认) : 256 (Xen默认) = 4:1
const DefaultCgroupShare = 1024
const DefaultXenWeight = 256
const ShareWeightRatio = DefaultCgroupShare / DefaultXenWeight
const balloonDriverName = "xen_balloon"

// ShareToWeight converts an OCI cpu.shares style value into the Xen credit2
// scheduler weight range [1, 65535]. When shares is 0 (unset) we fall back to
// Xen's default weight.
// ShareToWeight converts cgroup CPU shares to Xen CPU weight.
// 映射关系：Container CPU Share (1024:256) -> RTOS Client CPU Weight
// 转换公式：weight = max(1, min(shares / 4, 65535))
// 范围：
//   - cgroup shares: 2-262144, default=1024
//   - Xen weight: 1-65535, default=256
func ShareToWeight(shares uint64) uint32 {
	if ShareWeightRatio <= 0 {
		return DefaultXenWeight
	}
	if shares == 0 {
		return DefaultXenWeight
	}
	weight := shares / uint64(ShareWeightRatio)
	if weight == 0 {
		return 1
	}
	if weight > 65535 {
		return 65535
	}
	return uint32(weight)
}

// xl info:
// `host                   : qemu-aarch64
// release                : 5.10.0-openeuler
// version                : #1 SMP PREEMPT Sat Jun 7 07:26:44 UTC 2025
// machine                : aarch64
// nr_cpus                : 3
// max_cpu_id             : 2
// nr_nodes               : 1
// cores_per_socket       : 1
// threads_per_core       : 1
// cpu_mhz                : 62.500
// hw_caps                : 00000000:00000000:00000000:00000000:00000000:00000000:00000000:00000000
// virt_caps              : hvm hap vpmu gnttab-v1
// arm_sve_vector_length  : 0
// total_memory           : 2048
// free_memory            : 1427
// sharing_freed_memory   : 0
// sharing_used_memory    : 0
// outstanding_claims     : 0
// free_cpus              : 0
// xen_major              : 4
// xen_minor              : 18
// xen_extra              : .2
// xen_version            : 4.18.2
// xen_caps               : xen-3.0-aarch64 xen-3.0-armv7l
// xen_scheduler          : credit2
// xen_pagesize           : 4096
// platform_params        : virt_start=0x0
// xen_changeset          :
// xen_commandline        : console=dtuart dtuart=/pl011Git commit '9000000' (see below for commit info) dom0_mem=512M
// cc_compiler            : aarch64-openeuler-linux-gnu-gcc (crosstool-NG 1.26.0) 12.3.1 20
// cc_compile_by          :
// cc_compile_domain      :
// cc_compile_date        : 2025-06-07
// build_id               : d54faddad0e57e72305a485d9b89288188c56ae8
// xend_config_format     : 4`

type XlInfo struct {
	host    string
	machine string
	// max physical cpus that Xen can handle
	nrCpus        uint32
	totalMemoryMB uint32
	freeMemoryMB  uint32
	xlver         string

	maxCpuId uint32
	// Cores per socket (NUMA/topology awareness)
	coresPerSocket uint32
	// Threads per core (SMT/hyperthreading info)
	threadsPerCore uint32
	cpuMhz         float64
	// number of cpus that are not allocated in **a cpu pool**
	freeCpus uint32

	xenCaps string
	// Scheduler type (credit, credit2, etc.)
	// decides in Xen building, default to be credit2 for now
	xenScheduler string
	xenPagesize  uint32
	virtCaps     string

	// Memory claims pending (affects available memory calculations)
	outstandingClaims uint64
	// Shared memory freed (memory reuse optimization)
	sharingFreedMemory uint64
	// Shared memory used (current shared memory usage)
	sharingUsedMemory uint64

	platformParams string
	// Xen boot parameters
	xenCommandline string

	// ARM-specific fields (for aarch64 systems - architecture optimizations)
	// turn off by default
	armSVEVectorLength uint32
}

//	xl vcpu-list Output Format
//	 Header Line:
//	 Name                              ID  VCPU  CPU  State  Time(s)  Affinity (Hard / Soft)
//	 Data Lines:
//	 <domain_name>                    <domid> <vcpuid> <cpu> <state> <time> <hard_affinity> / <soft_affinity>
//	 Field Details:
//	 1. Name (32 chars, left-aligned): Domain name
//	 2. ID (5 chars, right-aligned): Domain ID (numeric)
//	 3. VCPU (5 chars, right-aligned): VCPU ID (numeric)
//	 4. CPU (5 chars, right-aligned):
//	   - If VCPU is offline: -
//	   - If VCPU is online: CPU number the VCPU is currently running on
//	 5. State (5 chars):
//	   - Format: XYZ- where:
//	       - X: r if running, - if not
//	     - Y: b if blocked, - if not
//	     - Z: - (always)
//	   - If VCPU is offline: ---
//	 6. Time(s) (9 chars, right-aligned): CPU time consumed by this VCPU in seconds (with 1 decimal place)
//	 7. Affinity (variable width):
//	   - Hard affinity: CPU bitmap showing which CPUs the VCPU is allowed to run on
//	   - Soft affinity: CPU bitmap showing preferred CPUs for the VCPU
//	   - Format: <hard_bitmap> / <soft_bitmap>
//	 Example Output:
//
// $xl vcpu-list
// Name                                ID  VCPU   CPU State   Time(s) Affinity (Hard / Soft)
// Domain-0                             0     0    1   -b-     271.1  all / all
// Domain-0                             0     1    0   r--     257.5  all / all
// Domain-0                             0     2    -   --p       0.0  all / all
type XlVcpuInfo struct {
	DomainVCPUMap map[string][]VCPUEntry
}

// VCPUEntry represents a single VCPU entry
type VCPUEntry struct {
	// short Id
	DomainName string
	// micran ignore it acutally;
	DomainID     int
	VCPUID       int
	CPU          int // -1 if offline
	State        string
	TimeSeconds  float64
	HardAffinity string
	SoftAffinity string
}

type xlSubCmd string

const (
	info        xlSubCmd = "info"
	vcpulist    xlSubCmd = "vcpu-list"
	vcpupin     xlSubCmd = "vcpu-pin"
	vcpuset     xlSubCmd = "vcpu-set"
	vmlist      xlSubCmd = "vm-list"
	pause       xlSubCmd = "pause"
	resume      xlSubCmd = "unpause"
	domid       xlSubCmd = "domid"
	memset      xlSubCmd = "mem-set"
	memmax      xlSubCmd = "mem-max"
	schedcredit xlSubCmd = "sched-credit2"
)

func newxl(subcmd xlSubCmd, args ...string) *exec.Cmd {
	cmdArgs := []string{string(subcmd)}
	cmdArgs = append(cmdArgs, args...)
	return exec.Command("xl", cmdArgs...)
}

func xlvcpu() (*XlVcpuInfo, error) {
	var cmd *exec.Cmd
	var out bytes.Buffer
	cmd = newxl(vcpulist)
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to run xl info: %v", err)
	}

	return parseXlVcpuInfo(out.String())
}

func XlVcpuList() (*XlVcpuInfo, error) {
	return xlvcpu()
}

func XlDomID(clientID string) (int, error) {
	var stdout, stderr bytes.Buffer
	cmd := newxl(domid, clientID)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return 0, fmt.Errorf("xl domid %s: %s", clientID, msg)
	}

	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return 0, fmt.Errorf("xl domid %s returned empty output", clientID)
	}

	domid, err := strconv.Atoi(out)
	if err != nil {
		return 0, fmt.Errorf("xl domid %s invalid output %q: %w", clientID, out, err)
	}

	return domid, nil
}

func xinfo() (*XlInfo, error) {
	cmd := newxl(info)

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to run xl info: %v", err)
	}

	return parseXlInfo(out.String())
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

		switch key {
		case "host":
			info.host = value
		case "machine":
			info.machine = value
		case "nr_cpus":
			nrCpus, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("failed to parse nr_cpus: %v", err)
			}
			info.nrCpus = uint32(nrCpus)
		case "total_memory":
			totalMemory, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("failed to parse total_memory: %v", err)
			}
			info.totalMemoryMB = uint32(totalMemory)
		case "free_memory":
			freeMemory, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("failed to parse free_memory: %v", err)
			}
			info.freeMemoryMB = uint32(freeMemory)
		case "xen_major":
			// Build xl version
			info.xlver = value
		case "xen_minor":
			if info.xlver != "" {
				info.xlver += "." + value
			}
		case "xen_extra":
			if info.xlver != "" {
				info.xlver += value
			}

		case "max_cpu_id":
			maxCpuId, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("failed to parse max_cpu_id: %v", err)
			}
			info.maxCpuId = uint32(maxCpuId)
		case "cores_per_socket":
			coresPerSocket, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("failed to parse cores_per_socket: %v", err)
			}
			info.coresPerSocket = uint32(coresPerSocket)
		case "threads_per_core":
			threadsPerCore, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("failed to parse threads_per_core: %v", err)
			}
			info.threadsPerCore = uint32(threadsPerCore)
		case "cpu_mhz":
			cpuMhz, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse cpu_mhz: %v", err)
			}
			info.cpuMhz = cpuMhz

		case "free_cpus":
			freeCpus, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("failed to parse free_cpus: %v", err)
			}
			info.freeCpus = uint32(freeCpus)

		case "xen_caps":
			info.xenCaps = value
		case "xen_scheduler":
			info.xenScheduler = value
		case "xen_pagesize":
			xenPagesize, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("failed to parse xen_pagesize: %v", err)
			}
			info.xenPagesize = uint32(xenPagesize)
		case "virt_caps":
			info.virtCaps = value

		case "outstanding_claims":
			outstandingClaims, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse outstanding_claims: %v", err)
			}
			info.outstandingClaims = outstandingClaims
		case "sharing_freed_memory":
			sharingFreedMemory, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse sharing_freed_memory: %v", err)
			}
			info.sharingFreedMemory = sharingFreedMemory
		case "sharing_used_memory":
			sharingUsedMemory, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse sharing_used_memory: %v", err)
			}
			info.sharingUsedMemory = sharingUsedMemory

		case "platform_params":
			info.platformParams = value
		case "xen_commandline":
			info.xenCommandline = value

		case "arm_sve_vector_length":
			armSVEVectorLength, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("failed to parse arm_sve_vector_length: %v", err)
			}
			info.armSVEVectorLength = uint32(armSVEVectorLength)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading xl info output: %v", err)
	}

	return info, nil
}

func parseXlVcpuInfo(output string) (*XlVcpuInfo, error) {
	info := &XlVcpuInfo{
		DomainVCPUMap: make(map[string][]VCPUEntry),
	}

	scanner := bufio.NewScanner(strings.NewReader(output))

	headerFound := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.Contains(line, "Name") && strings.Contains(line, "Affinity") {
			headerFound = true
			break
		}
	}

	if !headerFound {
		return nil, fmt.Errorf("could not find vcpu-list header")
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		vcpu, err := parseVcpuLine(line)
		if err != nil {
			return nil, fmt.Errorf("error parsing line '%s': %v", line, err)
		}

		info.DomainVCPUMap[vcpu.DomainName] = append(info.DomainVCPUMap[vcpu.DomainName], vcpu)
	}

	return info, nil
}

func parseVcpuLine(line string) (VCPUEntry, error) {
	// line format: Name(32) ID(5) VCPU(5) CPU(5) State(5) Time(9) Affinity(variable)
	re := regexp.MustCompile(`^(\S.{31})\s+(\d+)\s+(\d+)\s+(\d+|-)\s+([r-])([b-])([-])\s+([\d.]+)\s+(.+)$`)
	matches := re.FindStringSubmatch(line)
	if matches == nil {
		return VCPUEntry{}, er.ErrOutputParse
	}

	domainName := strings.TrimSpace(matches[1])
	domainID, _ := strconv.Atoi(matches[2])
	vcpuid, _ := strconv.Atoi(matches[3])

	cpu := -1
	if matches[4] != "-" {
		cpu, _ = strconv.Atoi(matches[4])
	}

	state := "---"
	if matches[4] != "-" {
		state = matches[5] + matches[6] + matches[7]
	}

	timeSeconds, _ := strconv.ParseFloat(matches[8], 64)

	affinity := matches[9]
	parts := strings.Split(affinity, " / ")
	hardAffinity := parts[0]
	softAffinity := ""
	if len(parts) > 1 {
		softAffinity = parts[1]
	}

	return VCPUEntry{
		DomainName:   domainName,
		DomainID:     domainID,
		VCPUID:       vcpuid,
		CPU:          cpu,
		State:        state,
		TimeSeconds:  timeSeconds,
		HardAffinity: hardAffinity,
		SoftAffinity: softAffinity,
	}, nil
}

func (xi *XlInfo) nodePhysicalCPUNum() uint32 {
	return xi.nrCpus
}

// MemoryMB returns the amount of free and total memory in MB.
func MemoryMB() (free, total uint32) {
	v, _ := mem.VirtualMemory()
	free = uint32(v.Free >> 20)   // Convert bytes to MB
	total = uint32(v.Total >> 20) // Convert bytes to MB

	i, err := xinfo()
	if err != nil {
		log.Debugf("failed to get machine info: %v", err)
		return free, total
	}
	free, total = i.freeMemoryMB, i.totalMemoryMB
	return free, total
}

var (
	maxCPUNum     uint32
	maxCPUNumOnce sync.Once
)

// MaxCPUNum returns the maximum number of CPUs available on the physical machine.
// This function uses sync.Once to ensure the value is calculated only once,
// as the physical CPU count is static and won't change during runtime.
func MaxCPUNum() uint32 {
	maxCPUNumOnce.Do(func() {
		i, err := xinfo()
		if err != nil {
			log.Debugf("failed to get machine info: %v", err)
			maxCPUNum = uint32(runtime.NumCPU())
		} else {
			maxCPUNum = i.nodePhysicalCPUNum()
		}
		log.Debugf("MaxCPUNum initialized to: %d", maxCPUNum)
	})

	return maxCPUNum
}

// XlMemSet sets memory for a domain using xl mem-set
func XlMemSet(domainName string, memMB int) error {
	cmd := newxl(memset, domainName, strconv.Itoa(memMB))
	log.Debugf("run %s to set memory to %d MB for domain %s", cmd.String(), memMB, domainName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xl mem-set failed for domain %s: %v", domainName, err)
	}
	log.Debugf("mem-set %d MB for domain %s successfully", memMB, domainName)
	return nil
}

// XlMemMax sets maximum memory for a domain using xl mem-max
func XlMemMax(domainName string, memMB int) error {
	cmd := newxl(memmax, domainName, strconv.Itoa(memMB))
	log.Debugf("run %s to set max memory to %d MB for domain %s", cmd.String(), memMB, domainName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xl mem-max failed for domain %s: %v", domainName, err)
	}
	log.Debugf("mem-max %d MB for domain %s successfully", memMB, domainName)
	return nil
}

// XlVcpuSet sets VCPU count for a domain using xl vcpu-set
func XlVcpuSet(domainName string, vcpuCount int) error {
	cmd := newxl(vcpuset, domainName, strconv.Itoa(vcpuCount))
	log.Debugf("run %s to set VCPU count to %d for domain %s", cmd.String(), vcpuCount, domainName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xl vcpu-set failed for domain %s: %v", domainName, err)
	}
	log.Debugf("vcpu-set %d for domain %s successfully", vcpuCount, domainName)
	return nil
}

// XlSchedCredit2 sets CPU weight and capacity for a domain using xl sched-credit2
func XlSchedCredit2(domainName string, weight, cap int) error {
	// Validate weight if a value is provided
	if weight != 0 && weight < 1 {
		return fmt.Errorf("CPU weight must be >= 1, got %d", weight)
	}

	var args []string
	args = append(args, "-d", domainName)

	// Only set weight if provided
	if weight > 0 {
		args = append(args, "-w", strconv.Itoa(weight))
	}

	// Only set cap if provided (and > 0)
	if cap > 0 {
		args = append(args, "-c", strconv.Itoa(cap))
	}

	cmd := newxl(schedcredit, args...)
	log.Debugf("run %s to set scheduler parameters for domain %s", cmd.String(), domainName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xl sched-credit2 failed for domain %s: %v", domainName, err)
	}
	log.Debugf("sched-credit2 set weight=%d, cap=%d for domain %s successfully", weight, cap, domainName)
	return nil
}

// For cases, id is truncated id
func Resume(id string) error {
	cmd := newxl(resume, id)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xl failed to resume %s: %v", id, err)
	}
	log.Debugf("resume %s successfully", id)
	return nil
}

func Pause(id string) error {
	cmd := newxl(pause, id)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xl failed to pause %s: %v", id, err)
	}
	log.Debugf("pause %s successfully", id)
	return nil
}

func XenDefaultPedConf() string {
	return "image.bin"
}

// assume cpu set is valid
// do hard affinity only
func PinVCPU(clientID, cpus string) error {
	cmd := newxl(vcpupin, clientID, "all", cpus)
	log.Debugf("run %s to pinning vcpu %s to %s", cmd.String(), cpus, clientID)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xl failed to pause %s: %v", clientID, err)
	}
	return nil
}

func MemLowThreshold() uint32 {
	return 2
}

func MemHighThreshold() uint32 {
	xi, err := xinfo()
	if err != nil {
		return 0
	}
	maxMem := xi.totalMemoryMB
	if maxMem < MemLowThreshold() {
		maxMem = MemLowThreshold() + 1
	}

	return maxMem
}

func ControlOSCpuset() cpuset.CPUSet {

	vcpuInfo, err := xlvcpu()
	if err != nil {
		log.Debugf("failed to get vcpu info: %v", err)
		return cpuset.NewCPUSet(0)
	}

	dom0VCPUs, exists := vcpuInfo.DomainVCPUMap["Domain-0"]
	if !exists {
		log.Debugf("Domain-0 not found in vcpu list")
		return cpuset.NewCPUSet(0)
	}

	cpuSet := cpuset.NewCPUSet()
	for _, vcpu := range dom0VCPUs {
		affinityCPUs, err := parseAffinity(vcpu.HardAffinity)
		if err != nil {
			log.Debugf("failed to parse affinity '%s': %v", vcpu.HardAffinity, err)
			continue
		}
		cpuSet = cpuSet.Union(affinityCPUs)
	}

	if cpuSet.Size() == 0 {
		return cpuset.NewCPUSet(0)
	}

	return cpuSet
}

func parseAffinity(affinity string) (cpuset.CPUSet, error) {
	if affinity == "all" {
		xlInfo, err := xinfo()
		if err != nil {
			return cpuset.NewCPUSet(0, 1, 2, 3), nil
		}
		cpuList := make([]int, xlInfo.nrCpus)
		for i := uint32(0); i < xlInfo.nrCpus; i++ {
			cpuList[i] = int(i)
		}
		return cpuset.NewCPUSet(cpuList...), nil
	}

	set, err := cpuset.Parse(affinity)
	if err != nil {
		return cpuset.NewCPUSet(), err
	}

	return set, nil
}

func DomainID(clientID string) (int, error) {
	if domid, err := XlDomID(clientID); err == nil {
		return domid, nil
	} else {
		log.Debugf("xl domid fallback failed for %s: %v", clientID, err)
	}

	return parseXLListForDomain(clientID)
}

func parseXLListForDomain(clientID string) (int, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("xl", "list")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return 0, fmt.Errorf("xl list failed: %s", msg)
	}

	scanner := bufio.NewScanner(strings.NewReader(stdout.String()))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Name") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		if fields[0] != clientID {
			continue
		}

		domid, err := strconv.Atoi(fields[1])
		if err != nil {
			return 0, fmt.Errorf("xl list returned invalid domid %q for %s: %w", fields[1], clientID, err)
		}
		return domid, nil
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("xl list parse error: %w", err)
	}

	return 0, fmt.Errorf("domain %s not found in xl list output", clientID)
}

const xenstorePathFmt = "/local/domain/%d/%s"

func xenStoreRead(name, item string) (string, error) {
	domId, err := XlDomID(name)
	if err != nil {
		return "", err
	}
	xenstoreKey := fmt.Sprintf(xenstorePathFmt, domId, item)
	out, err := xenStoreReadRaw(xenstoreKey)
	if err != nil {
		return "", err
	}
	return out, nil
}

func xenStoreReadRaw(key string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("xenstore-read", key)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("xenstore-read %s: %s", key, msg)
	}

	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return "", fmt.Errorf("xenstore-read %s returned empty output", key)
	}
	return out, nil
}

// XenStoreReadDomainState reads the domain state from xenstore
// Returns "running" if domain is active and ready, otherwise returns the actual state
func XenStoreReadDomainState(name string) (string, error) {
	// Try to read domain state from xenstore
	// Xen stores domain state as a number, but we can also check via xl list
	domId, err := XlDomID(name)
	if err != nil {
		return "", err
	}

	// Check if domain exists and is running via xl list
	var stdout bytes.Buffer
	cmd := exec.Command("xl", "list", fmt.Sprintf("%d", domId))
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to check domain %s state: %w", name, err)
	}

	output := stdout.String()
	lines := strings.Split(output, "\n")

	// Parse xl list output - format: Name ID Mem VCPUs State Time(s)
	// State format: r----- (running), ---s-- (shutdown), etc.
	for _, line := range lines {
		if strings.Contains(line, fmt.Sprintf("%d", domId)) {
			fields := strings.Fields(line)
			if len(fields) >= 5 {
				state := fields[4]
				// If first character is 'r', domain is running
				if len(state) > 0 && state[0] == 'r' {
					return "running", nil
				}
				return state, nil
			}
		}
	}

	return "", fmt.Errorf("domain %s not found in xl list", name)
}
