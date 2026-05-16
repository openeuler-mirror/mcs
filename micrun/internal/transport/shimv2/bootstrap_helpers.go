package shim

import (
	"context"
	"fmt"

	oci "micrun/internal/adapters/config/oci"
	libmica "micrun/internal/adapters/guest/libmica"
	cntr "micrun/internal/domain/container"
	"micrun/internal/support/contextx"
	log "micrun/internal/support/logger"

	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
)

// Generate the socket address for a pod managed by this shim.
// For regular containers and sandboxes, the address will be handled in Create().
func getContainerSocketAddr(ctx context.Context, bundle string, opts shimv2.StartOpts) (string, error) {
	ctx = contextx.OrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	ociSpec, err := oci.LoadSpec(bundle)
	if err != nil {
		return "", fmt.Errorf("failed to load valid runtime config: %w", err)
	}

	ctype, err := oci.GetContainerType(&ociSpec)
	if err != nil {
		return "", err
	}

	if ctype == cntr.PodContainer {
		sandboxID, err := oci.GetSandboxID(&ociSpec)
		if err != nil {
			return "", err
		}
		sockAddr, err := shimv2.SocketAddress(ctx, opts.Address, sandboxID)
		if err != nil {
			return "", fmt.Errorf("failed to generate socket address: %w", err)
		}
		return sockAddr, nil
	}
	return "", nil
}

func tipSchedCore() {
	log.Infof("Sched core is enabled, but micrun does not need it.")
	log.Debugf(`The functions and features of SCHED_CORE can completely be replaced by Pedestal due to Mica architecture.
	Hence micrun does not need it for now.
	However, we may implement more features about pedestal scheduling algos, not relying on only Xen hypervisor ??`)
}

func getMicadPid() (int, error) {
	pid, err := libmica.MicadDetect()
	if err != nil {
		return 0, fmt.Errorf("micad not running: %w", err)
	}
	if pid == 0 {
		return 0, fmt.Errorf("micad not running")
	}
	return pid, nil
}
