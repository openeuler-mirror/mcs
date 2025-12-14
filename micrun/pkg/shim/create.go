package shim

import (
	"context"
	"fmt"
	defs "micrun/definitions"
	log "micrun/logger"
	cntr "micrun/pkg/micantainer"
	oci "micrun/pkg/oci"
	"micrun/pkg/pedestal"
	"micrun/pkg/utils"
	"path/filepath"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/mount"
	crioption "github.com/containerd/containerd/pkg/runtimeoptions/v1"
	"github.com/containerd/typeurl/v2"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// create is the internal implementation for the Create RPC. It handles sandbox and container creation.
//
// This function orchestrates the entire container/sandbox creation flow, translating containerd requests
// into mica runtime operations. The process involves: **file paths are shown for example**
//
//  1. State Directory Setup: Ensures /tmp/micrun (MicrunContainerStateDir) exists for runtime state.
//     This directory stores runtime metadata and facilitates communication with mica daemon.
//
//  2. Rootfs Processing: Extracts filesystem mount information from the request.
//     Typically r.Rootfs contains one mount point with source like:
//     "/var/lib/containerd/io.containerd.snapshotter.v1.overlayfs/snapshots/123/fs"
//     micrun needs to mount it into container directory at "<bundle>/rootfs", see below:
//
//  3. OCI Spec Loading: Loads the OCI runtime specification from config.json in the <bundle> directory.
//     Bundle path structure: "/var/lib/containerd/io.containerd.runtime.v2.task/<namespace>/<container_id>"
//     Example: "/var/lib/containerd/io.containerd.runtime.v2.task/default/test"
//
//  4. Container Type Detection: Determines if this is a PodSandbox (pause container),
//     SingleContainer (ctr/nerdctl created container), or PodContainer (container within a pod, where a sandbox is created).
//     This detection is based on OCI image annotations and CRI configurations.
//
//  5. Runtime Configuration: Loads runtime config from multiple sources with precedence:
//     Micrun shim model is 1:1:1 (Shim : Container : RTOS), so runtime config is for per-shim, not traditional *daemon* level.
//     Hence we support customize per-shim config via annotations or CRI options, not only files
//     - Annotations (highest priority, e.g., "org.openeuler.mica.pedestal=xen")
//     - CRI Options from containerd
//     - Environment variables (defs.MicrunConfEnv)
//     - Default config files in standard locations
//
//  6. Rootfs Mounting: Mounts the container's root filesystem to "<bundle>/rootfs".
//     For non-sandbox containers, traverses and logs the rootfs contents for debugging.
//
// 7. Sandbox/Container Creation: Based on container type:
//
//   - PodSandbox/SingleContainer: Creates new sandbox (calls createSandboxContainer)
//
//   - PodContainer: Adds container to existing sandbox (calls createPodContainer)
//
//     8. Network Namespace Setup: For sandboxes, creates a network namespace managed by nerdctl.
//     The namespace path is stored in annotations as "nerdctl/network-namespace".
//     Network namespace holder PID is tracked for proper lifecycle management.
//
//     9. Container Object Creation: Instantiates the container object with proper typing and,
//     for sandboxes, stores the netns holder PID for state queries.
//
// Typical variable values:
//   - r.ID: "test" (containerd-assigned unique ID)
//   - r.Bundle: "/run/containerd/io.containerd.runtime.v2.task/default/test"
//   - rootfsPath: "/run/containerd/io.containerd.runtime.v2.task/default/test/rootfs"
//   - containerType: cntr.PodSandbox (for pause), cntr.SingleContainer (ctr create), or cntr.PodContainer
//   - Runtime config source: annotation like "org.openeuler.micrun.pedestal=xen"
//
// Returns a container object representing the created sandbox or container, or an error
// if any step in the creation process fails. The function ensures proper cleanup of
// partially created resources on error.
func create(ctx context.Context, s *shimService, r *taskAPI.CreateTaskRequest) (*shimContainer, error) {

	rootfs := cntr.RootFs{}
	// the first of r.Rootfs is the bundle rootfs
	if len(r.Rootfs) == 1 {
		mnt := r.Rootfs[0]
		rootfs.Source = mnt.Source
		rootfs.Type = mnt.Type
		rootfs.Options = mnt.Options
	}

	detach := r.Terminal
	ociSpec, bundlePath, err := loadSpec(r.ID, r.Bundle)

	if err != nil {
		return nil, err
	}

	containerType, err := oci.GetContainerType(ociSpec)
	if err != nil {
		return nil, err
	}

	disableOutput := detach && ociSpec.Process.Terminal
	rootfsPath := filepath.Join(r.Bundle, "rootfs")
	runtimeConfig, err := loadRuntimeConfig(s, r, ociSpec.Annotations)

	if err := setupContainer(ctx, s, containerType, r, ociSpec, runtimeConfig, bundlePath, rootfsPath, disableOutput, &rootfs); err != nil {
		return nil, err
	}

	container, err := newContainer(s, r, containerType, ociSpec, rootfs.Mounted)
	if err != nil {
		return nil, err
	}

	if containerType == cntr.PodSandbox && s.sandbox != nil {
		if pid := s.sandbox.NetnsHolderPID(); pid > 0 {
			container.pid = uint32(pid)
		}
	}

	return container, nil
}

func setupContainer(ctx context.Context, s *shimService, containerType cntr.ContainerType,
	r *taskAPI.CreateTaskRequest, ociSpec *specs.Spec, runtimeConfig *oci.RuntimeConfig,
	bundlePath, rootfsPath string, disableOutput bool, rootfs *cntr.RootFs) error {
	switch containerType {
	case cntr.PodSandbox, cntr.SingleContainer:
		return createSandboxContainer(ctx, s, containerType, r, ociSpec, runtimeConfig, bundlePath, rootfsPath, disableOutput, rootfs)
	case cntr.PodContainer:
		return createPodContainer(ctx, s, r, ociSpec, bundlePath, rootfsPath, disableOutput, rootfs)
	default:
		return fmt.Errorf("unsupported container type: %v", containerType)
	}
}

func createSandboxContainer(ctx context.Context, s *shimService, containerType cntr.ContainerType,
	r *taskAPI.CreateTaskRequest, ociSpec *specs.Spec, runtimeConfig *oci.RuntimeConfig,
	bundlePath, rootfsPath string, disableOutput bool, rootfs *cntr.RootFs) (err error) {
	if s.sandbox != nil {
		return fmt.Errorf("cannot create an existing sandbox: %s", s.sandbox.SandboxID())
	}

	s.config = runtimeConfig

	if containerType != cntr.PodSandbox {
		log.Debug("rootfs mounted for single container, showing rootfs contents:")
		utils.TravelDir(r.Rootfs[0].GetSource())
	}

	if errC := mountRootfs(rootfsPath, r.Rootfs); errC != nil {
		return errC
	}
	rootfs.Mounted = true

	defer func() {
		if err != nil && rootfs.Mounted {
			if errUmnt := mount.UnmountAll(rootfsPath, 0); errUmnt != nil {
				log.Warnf("failed to clean up rootfs mount: %v", errUmnt)
			}
		}
	}()

	if containerType != cntr.PodSandbox {
		log.Debug("rootfs mounted for single container, showing rootfs contents:")
		utils.TravelDir(rootfsPath)
	}

	var sandbox cntr.SandboxTraits
	sandbox, err = createSandbox(ctx, ociSpec, runtimeConfig, *rootfs, r.ID, bundlePath, disableOutput)
	if err != nil {
		return err
	}

	s.sandbox = sandbox
	return nil
}

func createPodContainer(ctx context.Context, s *shimService, r *taskAPI.CreateTaskRequest,
	ociSpec *specs.Spec, bundlePath, rootfsPath string,
	disableOutput bool, rootfs *cntr.RootFs) (err error) {
	if s.sandbox == nil {
		return fmt.Errorf("cannot start the pod container, since the sandbox is not created")
	}

	if errC := mountRootfs(rootfsPath, r.Rootfs); errC != nil {
		return errC
	}
	rootfs.Mounted = true

	defer func() {
		if err != nil && rootfs.Mounted {
			if errUmnt := mount.UnmountAll(rootfsPath, 0); errUmnt != nil {
				log.Warnf("Failed to cleanup rootfs mount: %v.", errUmnt)
			}
		}
	}()

	log.Debug("rootfs mounted for pod container, showing rootfs contents: ")

	return createPodContainerInSandbox(ctx, s.sandbox, *ociSpec, *rootfs, r.ID, bundlePath, s.config, disableOutput)
}

// mountRootfs mounts the container's root filesystem.
func mountRootfs(rootfsPath string, rootfs []*types.Mount) error {
	// NOTICE: Only one rootfs is supported.
	if len(rootfs) != 1 {
		log.Warnf("Only support one rootfs in bundle.")
	}

	if err := utils.MountDirs(rootfs, rootfsPath); err != nil {
		return err
	}
	return nil
}

// createPodContainerInSandbox creates a container within an existing sandbox.
// TODO: if ped=xen, cpupool is great to use
func createPodContainerInSandbox(ctx context.Context, sandbox cntr.SandboxTraits,
	ocispec specs.Spec, rootfs cntr.RootFs,
	containerID, bundlePath string, runtimeConfig *oci.RuntimeConfig, disableOutput bool) error {

	var defaultFirmware string
	if sandbox != nil {
		if fw, err := sandbox.Annotation(defs.FirmwarePathAnno); err == nil {
			defaultFirmware = fw
		}
	}

	containerConfig, err := oci.ParseContainerCfg(containerID, bundlePath, ocispec, cntr.PodContainer, disableOutput, defaultFirmware, runtimeConfig)
	if err != nil {
		return fmt.Errorf("failed to create container config: %w", err)
	}

	containerConfig.Rootfs = rootfs

	if err := validateFirmwareForContainer(containerConfig); err != nil {
		return fmt.Errorf("firmware validation failed for container %s: %w", containerID, err)
	}

	_, err = sandbox.CreateContainer(ctx, *containerConfig)
	if err != nil {
		return fmt.Errorf("failed to create container in sandbox: %w", err)
	}

	return nil
}

// createSandbox initializes and creates a new sandbox instance.
func createSandbox(ctx context.Context, ocispec *specs.Spec,
	runtimeConfig *oci.RuntimeConfig, rootfs cntr.RootFs,
	containerId, bundle string, disableOutput bool) (_ cntr.SandboxTraits, err error) {

	sandboxConfig, err := oci.SandboxConfig(ocispec, *runtimeConfig, bundle, containerId, disableOutput)
	if err != nil {
		return nil, err
	}

	if !rootfs.Mounted && len(sandboxConfig.ContainerConfigs) == 1 {
		if rootfs.Source != "" {
			realPath, err := utils.ResolvePath(rootfs.Source)
			if err != nil {
				return nil, err
			}
			rootfs.Source = realPath
		}
		sandboxConfig.ContainerConfigs[containerId].Rootfs = rootfs
	}

	if err := setupNetNS(sandboxConfig.ID, &sandboxConfig.NetworkConfig); err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			if ex := cleanupNetNS(sandboxConfig.ID, &sandboxConfig.NetworkConfig); ex != nil {
				log.Debugf("Failed to cleanup network namespace for sandbox %s: %v", sandboxConfig.ID, ex)
			}
		}
	}()

	if ocispec.Annotations == nil {
		ocispec.Annotations = make(map[string]string)
	}

	// NOTICE: nerdctl is considered as one of the first-class citizens of openEuler Embedded container engines
	// openEuler Embedded now supports containerd + nerdctl and docker-ce is not integrated in yocto, while user can install it via oebridge
	ocispec.Annotations["nerdctl/network-namespace"] = sandboxConfig.NetworkConfig.NetworkID
	sandboxConfig.Annotations["nerdctl/network-namespace"] = ocispec.Annotations["nerdctl/network-namespace"]
	sandbox, err := cntr.CreateSandbox(ctx, &sandboxConfig)
	if err != nil {
		return nil, err
	}

	log.Debugf("Sandbox <%s> created.", sandbox.SandboxID())
	containers := sandbox.GetAllContainers()

	if len(containers) != 1 {
		return nil, fmt.Errorf("container list from sandbox is wrong, expecting only one container, got %d", len(containers))
	}
	return sandbox, nil
}

func validateFirmwareForContainer(config *cntr.ContainerConfig) error {
	if config.IsInfra {
		log.Debugf("skipping firmware validation for infra container")
		return nil
	}

	// TODO: use multierr
	var err error
	if cntr.HostPedType == pedestal.Xen {
		if err = validate(config.PedestalConf); err != nil {
			log.Errorf("xen image file validation failed %v", err)
		}
	}
	err = validate(config.ImageAbsPath)
	if err != nil {
		return fmt.Errorf("failed to validate contaienr image files: %v", err)
	}

	return nil
}

func validate(p string) error {
	_, err := utils.EnsureRegularFilePath(p)
	return err
}

// getConfigPathFromOptions extracts the config path from CRI options.
func getConfigPathFromOptions(options typeurl.Any) (string, error) {
	v, err := typeurl.UnmarshalAny(options)
	if err != nil {
		return "", err
	}

	// Try current CRI options format.
	if option, ok := v.(*crioption.Options); ok {
		return option.ConfigPath, nil
	}

	// Optional backward compatibility via build tag 'oldcri'.
	if p, ok := getConfigPathFromOldCRI(v); ok {
		return p, nil
	}

	return "", nil
}
