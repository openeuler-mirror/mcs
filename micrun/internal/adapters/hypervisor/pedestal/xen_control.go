package pedestal

import (
	"context"
	"fmt"
	"strconv"

	log "micrun/internal/support/logger"
)

func XlMemSet(ctx context.Context, domainName string, memMB int) error {
	cmd := newXLContext(ctx, memset, domainName, strconv.Itoa(memMB))
	log.Debugf("run %s to set memory to %d MB for domain %s", cmd.String(), memMB, domainName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xl mem-set failed for domain %s: %w", domainName, err)
	}
	log.Debugf("mem-set %d MB for domain %s successfully", memMB, domainName)
	return nil
}

func XlMemMax(ctx context.Context, domainName string, memMB int) error {
	cmd := newXLContext(ctx, memmax, domainName, strconv.Itoa(memMB))
	log.Debugf("run %s to set max memory to %d MB for domain %s", cmd.String(), memMB, domainName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xl mem-max failed for domain %s: %w", domainName, err)
	}
	log.Debugf("mem-max %d MB for domain %s successfully", memMB, domainName)
	return nil
}

func xlVcpuSet(ctx context.Context, domainName string, vcpuCount int) error {
	cmd := newXLContext(ctx, vcpuset, domainName, strconv.Itoa(vcpuCount))
	log.Debugf("run %s to set VCPU count to %d for domain %s", cmd.String(), vcpuCount, domainName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xl vcpu-set failed for domain %s: %w", domainName, err)
	}
	log.Debugf("vcpu-set %d for domain %s successfully", vcpuCount, domainName)
	return nil
}

func XlSchedCredit2(ctx context.Context, domainName string, weight, cap int) error {
	if weight != 0 && weight < 1 {
		return fmt.Errorf("CPU weight must be >= 1, got %d", weight)
	}

	args := []string{"-d", domainName}
	if weight > 0 {
		args = append(args, "-w", strconv.Itoa(weight))
	}
	if cap > 0 {
		args = append(args, "-c", strconv.Itoa(cap))
	}

	cmd := newXLContext(ctx, schedcredit, args...)
	log.Debugf("run %s to set scheduler parameters for domain %s", cmd.String(), domainName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xl sched-credit2 failed for domain %s: %w", domainName, err)
	}
	log.Debugf("sched-credit2 set weight=%d, cap=%d for domain %s successfully", weight, cap, domainName)
	return nil
}

func Resume(ctx context.Context, id string) error {
	cmd := newXLContext(ctx, resume, id)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xl failed to resume %s: %w", id, err)
	}
	log.Debugf("resume %s successfully", id)
	return nil
}

func Pause(ctx context.Context, id string) error {
	cmd := newXLContext(ctx, pause, id)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xl failed to pause %s: %w", id, err)
	}
	log.Debugf("pause %s successfully", id)
	return nil
}

func XenDefaultPedConf() string {
	return "image.bin"
}

func PinVCPU(ctx context.Context, clientID, cpus string) error {
	cmd := newXLContext(ctx, vcpupin, clientID, "all", cpus)
	log.Debugf("run %s to pinning vcpu %s to %s", cmd.String(), cpus, clientID)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xl failed to pin vcpu for %s: %w", clientID, err)
	}
	return nil
}
