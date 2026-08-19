package pedestal

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"micrun/internal/support/cpuset"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
)

func xlvcpu(ctx context.Context) (*XlVcpuInfo, error) {
	cmd := newXLContext(ctx, vcpulist)

	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to run xl info: %w", err)
	}

	return parseXlVcpuInfo(out.String())
}

func xlVcpuList(ctx context.Context) (*XlVcpuInfo, error) {
	return xlvcpu(ctx)
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
			return nil, fmt.Errorf("error parsing line '%s': %w", line, err)
		}

		info.DomainVCPUMap[vcpu.DomainName] = append(info.DomainVCPUMap[vcpu.DomainName], vcpu)
	}

	return info, scanner.Err()
}

func parseVcpuLine(line string) (VCPUEntry, error) {
	fields := strings.Fields(line)
	// Newer xl prints a two-column "Hard/Soft" affinity; older versions a
	// single affinity column (7 fields total, fields[6:] is the affinity).
	if len(fields) < 7 {
		return VCPUEntry{}, er.ErrOutputParse
	}

	domainName := fields[0]
	domainID, err := strconv.Atoi(fields[1])
	if err != nil {
		return VCPUEntry{}, er.ErrOutputParse
	}
	vcpuid, err := strconv.Atoi(fields[2])
	if err != nil {
		return VCPUEntry{}, er.ErrOutputParse
	}

	cpu := -1
	if fields[3] != "-" {
		cpu, err = strconv.Atoi(fields[3])
		if err != nil {
			return VCPUEntry{}, er.ErrOutputParse
		}
	}

	state := fields[4]
	if len(state) != 3 {
		return VCPUEntry{}, er.ErrOutputParse
	}

	timeSeconds, err := strconv.ParseFloat(fields[5], 64)
	if err != nil {
		return VCPUEntry{}, er.ErrOutputParse
	}

	affinity := strings.Join(fields[6:], " ")
	parts := strings.SplitN(affinity, "/", 2)
	hardAffinity := strings.TrimSpace(parts[0])
	softAffinity := ""
	if len(parts) > 1 {
		softAffinity = strings.TrimSpace(parts[1])
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

func ControlOSCpuset(ctx context.Context) cpuset.CPUSet {
	vcpuInfo, err := xlvcpu(ctx)
	if err != nil {
		// Do not fall back to cpuset.NewCPUSet(0): a transient xl failure
		// masquerading as "Dom0 pinned to CPU 0" would make every later CPU
		// allocation believe only CPU 0 is available (the same hazard
		// parseAffinity explicitly avoids). Return an empty set and let the
		// caller decide; surface the failure at Warn so it is not invisible.
		log.Warnf("failed to get vcpu info for Dom0 cpuset: %v", err)
		return cpuset.NewCPUSet()
	}

	dom0VCPUs, exists := vcpuInfo.DomainVCPUMap["Domain-0"]
	if !exists {
		log.Warnf("Domain-0 not found in vcpu list; returning empty host cpuset")
		return cpuset.NewCPUSet()
	}

	cpuSet := cpuset.NewCPUSet()
	for _, vcpu := range dom0VCPUs {
		affinityCPUs, err := parseAffinity(ctx, vcpu.HardAffinity)
		if err != nil {
			log.Warnf("failed to parse affinity '%s': %v", vcpu.HardAffinity, err)
			continue
		}
		cpuSet = cpuSet.Union(affinityCPUs)
	}

	if cpuSet.Size() == 0 {
		log.Warnf("Dom0 cpuset resolved to empty after parsing all vcpus")
		return cpuset.NewCPUSet()
	}
	return cpuSet
}

func parseAffinity(ctx context.Context, affinity string) (cpuset.CPUSet, error) {
	// Older xl prints "any cpu" for an unpinned vcpu; treat it like "all".
	if affinity == "all" || affinity == "any cpu" {
		xlInfo, err := xinfo(ctx)
		if err != nil {
			// Do not silently fall back to a single-CPU set: that would make
			// every later CPU allocation believe only CPU 0 is available.
			return cpuset.NewCPUSet(), fmt.Errorf("failed to get xl info for affinity: %w", err)
		}
		if xlInfo.nrCpus == 0 || xlInfo.nrCpus > 1024 {
			return cpuset.NewCPUSet(), fmt.Errorf("invalid CPU count from xl info: %d", xlInfo.nrCpus)
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
