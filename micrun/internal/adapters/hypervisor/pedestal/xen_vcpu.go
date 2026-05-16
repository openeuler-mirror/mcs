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
	if len(fields) < 8 {
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
		affinityCPUs, err := parseAffinity(ctx, vcpu.HardAffinity)
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

func parseAffinity(ctx context.Context, affinity string) (cpuset.CPUSet, error) {
	if affinity == "all" {
		xlInfo, err := xinfo(ctx)
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
