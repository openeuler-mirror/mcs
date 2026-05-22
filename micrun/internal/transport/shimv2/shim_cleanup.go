package shim

import (
	"context"
	"fmt"
	"os"
	"time"

	oci "micrun/internal/adapters/config/oci"
	cntr "micrun/internal/domain/container"
	log "micrun/internal/support/logger"
	"micrun/internal/support/timex"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *shimService) Cleanup(ctx context.Context) (*taskAPI.DeleteResponse, error) {
	logrus.SetOutput(os.Stderr)
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	if s.id == "" {
		return nil, fmt.Errorf("container ID is required")
	}

	ociSpec, err := oci.LoadSpec(cwd)
	if err != nil {
		return nil, fmt.Errorf("failed to load valid runtime config: %w", err)
	}

	if err := s.cleanupByContainerType(ctx, cwd, &ociSpec); err != nil {
		return nil, err
	}

	return cleanupDeleteResponse(timex.Now(s.now)), nil
}

func cleanupExitStatus() uint32 {
	return 128 + uint32(unix.SIGKILL)
}

func cleanupDeleteResponse(exitedAt time.Time) *taskAPI.DeleteResponse {
	return deleteResponse(cleanupExitStatus(), timestamppb.New(exitedAt), 0)
}

func (s *shimService) cleanupByContainerType(ctx context.Context, cwd string, ociSpec *specs.Spec) error {
	ctype, err := oci.GetContainerType(ociSpec)
	if err != nil {
		return err
	}

	switch ctype {
	case cntr.PodSandbox, cntr.SingleContainer:
		return cleanupContainer(ctx, s.runtimeDeps.guestControl, s.runtimeDeps.containerDeps, s.id, s.id, cwd)
	case cntr.PodContainer:
		sandboxID, err := oci.GetSandboxID(ociSpec)
		if err != nil {
			return err
		}
		return cleanupContainer(ctx, s.runtimeDeps.guestControl, s.runtimeDeps.containerDeps, sandboxID, s.id, cwd)
	default:
		log.Tracef("unknown container type to be cleaned up: %s", ctype)
		return nil
	}
}
