package shim

import (
	"fmt"

	runtimecfg "micrun/internal/adapters/config/runtimeconfig"
	cntr "micrun/internal/domain/container"
	"micrun/internal/ports"
	"micrun/internal/support/timex"
	"micrun/internal/support/validation"
)

type runtimeDependencies struct {
	guestControl    ports.GuestControl
	hypervisor      ports.HypervisorControl
	resourcePolicy  cntr.ResourcePolicy
	runtimeResolver runtimecfg.Resolver
	containerDeps   *cntr.Dependencies
	processID       processIDProvider
	shutdown        shutdownEffects
	now             timex.Clock
}

func buildRuntimeDependencies(bindings runtimeEnvironment, containerDeps *cntr.Dependencies) runtimeDependencies {
	return runtimeDependencies{
		guestControl:    bindings.guestControl,
		hypervisor:      bindings.hypervisor,
		resourcePolicy:  cntr.ResourcePolicyFromDependencies(containerDeps),
		runtimeResolver: runtimecfg.NewResolver(bindings.hostProfile),
		containerDeps:   containerDeps,
	}
}

func (d runtimeDependencies) validate() error {
	if err := validation.RequireAll("runtime dependencies are incomplete",
		validation.Required("guest control", d.guestControl),
		validation.Required("hypervisor control", d.hypervisor),
		validation.Required("container dependencies", d.containerDeps),
	); err != nil {
		return err
	}
	if err := d.containerDeps.Validate(); err != nil {
		return fmt.Errorf("container dependencies are invalid: %w", err)
	}
	if err := d.resourcePolicy.Validate(); err != nil {
		return fmt.Errorf("resource policy is invalid: %w", err)
	}
	return nil
}
