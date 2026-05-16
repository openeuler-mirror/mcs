package pedestal

const DefaultCgroupShare = 1024
const DefaultXenWeight = 256
const ShareWeightRatio = DefaultCgroupShare / DefaultXenWeight
const balloonDriverName = "xen_balloon"

// ShareToWeight converts cgroup CPU shares to Xen CPU weight.
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

type XlInfo struct {
	host               string
	machine            string
	nrCpus             uint32
	totalMemoryMB      uint32
	freeMemoryMB       uint32
	xlver              string
	maxCpuId           uint32
	coresPerSocket     uint32
	threadsPerCore     uint32
	cpuMhz             float64
	freeCpus           uint32
	xenCaps            string
	xenScheduler       string
	xenPagesize        uint32
	virtCaps           string
	outstandingClaims  uint64
	sharingFreedMemory uint64
	sharingUsedMemory  uint64
	platformParams     string
	xenCommandline     string
	armSVEVectorLength uint32
}

type XlVcpuInfo struct {
	DomainVCPUMap map[string][]VCPUEntry
}

type VCPUEntry struct {
	DomainName   string
	DomainID     int
	VCPUID       int
	CPU          int
	State        string
	TimeSeconds  float64
	HardAffinity string
	SoftAffinity string
}
