package container

import (
	"context"
	"fmt"

	"github.com/opencontainers/runtime-spec/specs-go"
)

type resourceOperation struct {
	ctx    context.Context
	config *ContainerConfig
	policy ResourcePolicy
}

func newResourceOperation(ctx context.Context, config *ContainerConfig, policy ResourcePolicy) (resourceOperation, error) {
	if config == nil {
		return resourceOperation{}, fmt.Errorf("container config is nil")
	}
	activeCtx, err := activeContainerContext(ctx)
	if err != nil {
		return resourceOperation{}, err
	}

	op := resourceOperation{
		ctx:    activeCtx,
		config: config,
		policy: policy,
	}
	if op.skipInfra() {
		return op, nil
	}
	if err := policy.Validate(); err != nil {
		return resourceOperation{}, err
	}
	return op, nil
}

func (op resourceOperation) skipInfra() bool {
	return op.config != nil && op.config.IsInfra
}

func normalizeResourceSpec(spec *specs.Spec) *specs.Spec {
	if spec == nil {
		return &specs.Spec{}
	}
	return spec
}
