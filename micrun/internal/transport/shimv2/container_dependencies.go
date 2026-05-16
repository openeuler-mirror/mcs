package shim

import (
	"context"
	"fmt"
	"strings"

	libmica "micrun/internal/adapters/guest/libmica"
	pedestal "micrun/internal/adapters/hypervisor/pedestal"
	statefile "micrun/internal/adapters/state/file"
	cntr "micrun/internal/domain/container"
	"micrun/internal/ports"
	defs "micrun/internal/support/definitions"
	"micrun/internal/support/fs"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func buildContainerDependencies(bindings runtimeEnvironment) *cntr.Dependencies {
	deps := &cntr.Dependencies{
		StateStoreFactory: runtimeStateStoreFactory(defs.MicrunStateDir),
		PlanEssentialRes: func(spec *specs.Spec) *cntr.ResourceChanges {
			return mapEssentialResources(spec, bindings.planEssentialResources)
		},
		MaxClientCPUs: bindings.maxClientCPUs,
		HostMemoryMiB: func(ctx context.Context) (uint32, uint32) {
			return bindings.hypervisor.MemoryMB(ctx)
		},
		HostMaxPhysCPUs: func(ctx context.Context) uint32 {
			return bindings.hypervisor.MaxCPUNum(ctx)
		},
		GuestExecutorFactory: func(id string) ports.GuestExecutor {
			return &libmica.MicaExecutor{ID: id, Hypervisor: bindings.hypervisor}
		},
		TTYDiscoveryRoots: runtimeTTYDiscoveryRoots(defs.MicrunStateDir),
		DefaultHypervisorControl: func() ports.HypervisorControl {
			return bindings.hypervisor
		},
		CreateGuest: mapCreateGuest,
		VCPUStats: func(ctx context.Context) (*cntr.VCPUUsageInfo, error) {
			info, err := bindings.vcpuStats(ctx)
			if err != nil {
				return nil, err
			}
			return mapVCPUUsageInfo(info), nil
		},
	}
	return deps
}

func configureRuntimePaths(deps *cntr.Dependencies, stateDir string) error {
	stateDir, err := normalizeRuntimeStateDir(stateDir)
	if err != nil {
		return err
	}
	if deps == nil {
		return nil
	}
	if err := setupStateDir(stateDir); err != nil {
		return err
	}
	deps.StateStoreFactory = runtimeStateStoreFactory(stateDir)
	deps.TTYDiscoveryRoots = runtimeTTYDiscoveryRoots(stateDir)
	return nil
}

func normalizeRuntimeStateDir(stateDir string) (string, error) {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		stateDir = defs.MicrunStateDir
	}
	clean, err := fs.CleanAbsolutePath(stateDir)
	if err != nil {
		return "", fmt.Errorf("runtime state directory is invalid: %w", err)
	}
	return clean, nil
}

func runtimeStateStoreFactory(stateDir string) func() ports.StateStore {
	return func() ports.StateStore {
		return statefile.New(stateDir)
	}
}

func runtimeTTYDiscoveryRoots(stateDir string) func() []string {
	return func() []string {
		return cntr.DefaultRPMSGTTYRoots(stateDir)
	}
}

func mapEssentialResources(spec *specs.Spec, planner func(*specs.Spec) *pedestal.EssentialResource) *cntr.ResourceChanges {
	if planner == nil {
		return cntr.NewResourceChanges()
	}

	resources := planner(spec)
	if resources == nil {
		return cntr.NewResourceChanges()
	}

	return &cntr.ResourceChanges{
		CPUCapacity:  resources.CPUCapacity,
		CPUWeight:    resources.CPUWeight,
		ClientCPUSet: resources.ClientCPUSet,
		VCPU:         resources.VCPU,
		MemoryMaxMB:  resources.MemoryMaxMB,
		MemoryMinMB:  resources.MemoryMinMB,
	}
}

func mapCreateGuest(ctx context.Context, conf cntr.GuestClientConfig) error {
	clientConf := libmica.MicaClientConf{}
	clientConf.InitWithOpts(libmica.MicaClientConfCreateOptions{
		CPU:             conf.CPU,
		CPUCapacity:     conf.CPUCapacity,
		CPUWeight:       conf.CPUWeight,
		VCPUs:           conf.VCPUs,
		MaxVCPUs:        conf.MaxVCPUs,
		MemoryMB:        conf.MemoryMB,
		MemoryThreshold: conf.MemoryThreshold,
		Name:            conf.Name,
		Path:            conf.Path,
		Ped:             conf.Ped,
		PedCfg:          conf.PedCfg,
	})
	return libmica.CreateContext(ctx, clientConf)
}

func mapVCPUUsageInfo(info *pedestal.XlVcpuInfo) *cntr.VCPUUsageInfo {
	if info == nil {
		return nil
	}

	result := &cntr.VCPUUsageInfo{
		DomainVCPUMap: make(map[string][]cntr.VCPUUsageEntry, len(info.DomainVCPUMap)),
	}
	for domain, entries := range info.DomainVCPUMap {
		domainEntries := make([]cntr.VCPUUsageEntry, 0, len(entries))
		for _, entry := range entries {
			domainEntries = append(domainEntries, cntr.VCPUUsageEntry{
				TimeSeconds: entry.TimeSeconds,
			})
		}
		result.DomainVCPUMap[domain] = domainEntries
	}
	return result
}
