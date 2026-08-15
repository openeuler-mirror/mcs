package pedestal

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	log "micrun/internal/support/logger"
)

func XlMemSet(ctx context.Context, domainName string, memMB int) error {
	// xl mem-set interprets a bare number as KiB; the "m" suffix selects
	// MiB (same convention as the C client, see library/remoteproc/
	// xen_rproc.c RSC_memory).
	cmd := newXLContext(ctx, memset, domainName, strconv.Itoa(memMB)+"m")
	log.Debugf("run %s to set memory to %d MB for domain %s", cmd.String(), memMB, domainName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xl mem-set failed for domain %s: %w", domainName, err)
	}
	log.Debugf("mem-set %d MB for domain %s successfully", memMB, domainName)
	return nil
}

func XlMemMax(ctx context.Context, domainName string, memMB int) error {
	// xl mem-max interprets a bare number as KiB; the "m" suffix selects
	// MiB (same convention as the C client, see library/remoteproc/
	// xen_rproc.c RSC_maxmemory).
	cmd := newXLContext(ctx, memmax, domainName, strconv.Itoa(memMB)+"m")
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

// XlSchedCredit2 sets credit2 scheduler parameters. weight<=0 leaves the
// weight untouched; cap<0 leaves the cap untouched. cap==0 is "unlimited"
// in credit2 and IS passed explicitly: omitting -c would turn the command
// into a pure query that exits 0 while the hypervisor keeps the old cap.
func XlSchedCredit2(ctx context.Context, domainName string, weight, cap int) error {
	if weight != 0 && weight < 1 {
		return fmt.Errorf("CPU weight must be >= 1, got %d", weight)
	}
	if cap < -1 {
		return fmt.Errorf("CPU cap must be >= -1, got %d", cap)
	}

	args := []string{"-d", domainName}
	if weight > 0 {
		args = append(args, "-w", strconv.Itoa(weight))
	}
	if cap >= 0 {
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

func xlDestroy(ctx context.Context, id string) error {
	var stderr bytes.Buffer
	cmd := newXLContext(ctx, destroy, id)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// Include stderr so a domain that vanished between two xl calls
		// ("... does not exist" / "not found") can be classified as a no-op
		// by isMissingDomainError instead of surfacing as a hard failure.
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("xl failed to destroy %s: %s", id, msg)
	}
	log.Debugf("destroy %s successfully", id)
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
