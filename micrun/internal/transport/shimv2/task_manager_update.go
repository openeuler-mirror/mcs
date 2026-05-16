package shim

import (
	"fmt"

	apptask "micrun/internal/application/task"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/typeurl/v2"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func updateInputFromTransport(r *taskAPI.UpdateTaskRequest) (apptask.UpdateInput, error) {
	resources, err := linuxResourcesFromUpdateRequest(r)
	if err != nil {
		return apptask.UpdateInput{}, err
	}
	if r == nil {
		return apptask.UpdateInput{Resources: resources}, nil
	}
	return apptask.UpdateInput{ID: r.ID, Resources: resources}, nil
}

func linuxResourcesFromUpdateRequest(r *taskAPI.UpdateTaskRequest) (specs.LinuxResources, error) {
	var res specs.LinuxResources
	if r == nil || r.Resources == nil {
		return res, nil
	}
	raw, err := typeurl.UnmarshalAny(r.Resources)
	if err != nil {
		return res, err
	}
	lr, ok := raw.(*specs.LinuxResources)
	if !ok || lr == nil {
		return res, fmt.Errorf("expected LinuxResources, got %T", raw)
	}
	return *lr, nil
}
