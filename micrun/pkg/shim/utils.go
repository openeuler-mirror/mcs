package shim

import (
	"context"
	"fmt"
	defs "micrun/definitions"
	er "micrun/errors"
	log "micrun/logger"
	"micrun/pkg/configstack"
	libmica "micrun/pkg/libmica"
	cntr "micrun/pkg/micantainer"
	"micrun/pkg/netns"
	oci "micrun/pkg/oci"
	"micrun/pkg/pedestal"
	"micrun/pkg/utils"
	"os"
	"path/filepath"
	"reflect"

	"github.com/containerd/cgroups"
	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/mount"
	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// Validate the bundle and rootfs.
func validBundle(containerID, bundlePath string) (string, error) {
	if containerID == "" {
		return "", fmt.Errorf("container ID is empty")
	}

	if bundlePath == "" {
		return "", fmt.Errorf("bundle path is required")
	}

	// resolve path first to handle symlinks before other checks
	resolved, err := utils.ResolvePath(bundlePath)
	if err != nil {
		return "", err
	}

	stat, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("invalid resolved bundle path '%s': %w", resolved, err)
	}
	if !stat.IsDir() {
		return "", fmt.Errorf("invalid resolved bundle path '%s', it should be a directory", resolved)
	}

	return resolved, nil
}

func validateRootfs(resolved string) error {
	// always mkdir rootfs inside bundle, whatever containerd use externalrootfs or not
	rootfs := filepath.Join(resolved, "rootfs")
	stat, err := os.Stat(rootfs)

	if err != nil && !os.IsNotExist(err) {
		log.Warnf("failed to stat rootfs")
	}

	if !stat.IsDir() || os.IsNotExist(err) {
		log.Warnf("rootfs path under '%s' is not a directory", resolved)
	}

	if err := setInternalRootfs(resolved); err != nil {
		return fmt.Errorf("failed to set internal rootfs \"%s\": %w", rootfs, err)
	}
	return nil
}

// The bundle is <CONTINAER_STATE_ROOT>/<container_id>.
func setInternalRootfs(bundle string) error {

	// config := filepath.Join(bundle, "config.json")
	rootfs := filepath.Join(bundle, "rootfs")

	// TODO: recursively chmod 0555
	if err := utils.SetReadonly(rootfs); err != nil {
		return fmt.Errorf("failed to chmod rootfs: %w", err)
	}
	os.Chdir(bundle)
	return nil
}

// Generate the socket address for a pod managed by this shim.
// For regular containers and sandboxes, the address will be handled in Create().
func getContainerSocketAddr(ctx context.Context, bundle string, opts shimv2.StartOpts) (string, error) {

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
		// format: unix://<run_root>/s/<sha256(..)>
		sockAddr, err := shimv2.SocketAddress(ctx, opts.Address, sandboxID)
		if err != nil {
			return "", fmt.Errorf("failed to generate socket address: %w", err)
		}
		return sockAddr, nil
	}
	return "", nil
}

// TODO:
// choose by priority:
// 1. runtime configurated
// 2. alternatives, in defs.
// 3. default k8s.gcr.io/pause, registry.k8s.io/pause, rancher.k8s.io/pause..
func getPausePatterns() []string {
	return []string{"pause", "/pause", defs.PauseImage, "registry.k8s.io/pause", "rancher.k8s.io/pause", "docker.io/pause"}
}

// Handle SCHED_CORE.
func tipSchedCore() {
	log.Infof("Sched core is enabled, but micrun does not need it.")
	log.Debugf(`The functions and features of SCHED_CORE can completely be replaced by Pedestal due to Mica architecture.
	Hence micrun does not need it for now.
	However, we may implement more features about pedestal scheduling algos, not relying on only Xen hypervisor ??`)
}

func getMicadPid() (int, error) {
	// Get MICA daemon state which includes PID
	daemonState, err := libmica.DaemonState()

	if err != nil {
		log.Warnf("Failed to get micad daemon state: %v", err)
		return 0, err
	}

	// Check if daemon is actually running before returning PID
	if daemonState.State != libmica.DaemonRunning {
		return 0, fmt.Errorf("Micad daemon is not running (state: %s)", daemonState.State)
	}

	return daemonState.Pid, nil
}

func cleanupContainer(ctx context.Context, sandboxID, containerID, bundle string) error {
	log.Debugf("cleanup container from sandbox %s, and remove rootfs of container %s", sandboxID, containerID)
	if err := cntr.CleanupContainer(ctx, sandboxID, containerID, false); err != nil {
		return fmt.Errorf("failed to cleanup container %s: %w", containerID, err)
	}

	rootfs := filepath.Join(bundle, "rootfs")
	if err := mount.UnmountAll(rootfs, 0); err != nil {
		log.Errorf("failed to umount: %s", rootfs)
		return err
	}
	return nil
}

// setupStateDir ensures the state directory for micran exists.
func setupStateDir() error {
	if err := os.MkdirAll(defs.MicrunStateDir, 0755); err != nil {
		return fmt.Errorf("failed to create micran state directory: %w", err)
	}
	return nil
}

func loadSpec(id, bundle string) (*specs.Spec, string, error) {
	bundle, err := validBundle(id, bundle)
	if err != nil {
		return nil, "", err
	}
	spec, err := oci.LoadSpec(bundle)
	if err != nil {
		return nil, "", err
	}

	return &spec, bundle, nil

}

func cgroupV1() (bool, error) {
	if cgroups.Mode() == cgroups.Legacy || cgroups.Mode() == cgroups.Hybrid {
		return true, nil
	} else if cgroups.Mode() == cgroups.Unified {
		return false, nil
	} else {
		return false, fmt.Errorf("get unknown cgroup mode")
	}
}

// loadRuntimeConfig loads the runtime configuration from annotations, CRI options, or environment variables.
func loadRuntimeConfig(s *shimService, r *taskAPI.CreateTaskRequest, annotations map[string]string) (*oci.RuntimeConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config != nil {
		return s.config, nil
	}

	stack := oci.NewRuntimeStack()

	// Config path precedence (high to low): annotations > CRI options > env.
	var (
		configPath string
		source     string
	)

	if v := oci.GetSandboxConfigPath(annotations); v != "" {
		configPath = v
		source = "annotation"
	} else if r.Options != nil {
		p, err := getConfigPathFromOptions(r.Options)
		if err != nil {
			return nil, err
		}
		if p != "" {
			configPath = p
			source = "options"
			log.Debugf("Parsed config path from options: %s.", configPath)
		}
	}

	if configPath == "" {
		if v := configstack.FirstNonEmptyEnv(defs.MicrunConfEnv); v != "" {
			configPath = v
			source = "env"
		}
	}

	if configPath != "" {
		parsed, err := loadConfigFromFile(configPath)
		if err != nil {
			if source == "env" {
				log.Warnf("Failed to load runtime config from %s (env): %v; using defaults.", configPath, err)
				stack.Replace(nil)
			} else {
				return nil, fmt.Errorf("failed to load runtime config from %s (%s): %w", configPath, source, err)
			}
		} else {
			stack.Replace(parsed)
		}
	} else {
		files, err := configstack.DiscoverMicrunConfigFiles()
		if err != nil {
			log.Warnf("micrun config discovery failed: %v", err)
		}
		stack.ApplyMicrunFiles(files)
	}

	// Apply annotations on top, as they have higher precedence.
	stack.ApplyAnnotations(annotations)
	cfg := stack.Config()
	pedestal.EnableDom0CPUExclusive(cfg.ExclusiveDom0CPU)

	s.config = cfg
	return s.config, nil
}

// loadConfigFromFile loads the runtime configuration from a TOML or INI file.
// BUG: Implement actual config file loading.
func loadConfigFromFile(configPath string) (*oci.RuntimeConfig, error) {
	cfg := oci.NewRuntimeConfig()
	if err := cfg.ParseRuntimeFromINI(configPath); err != nil {
		return nil, err
	}
	return cfg, nil
}

// TODO: bad implementation, update it
func setupNetNS(sandboxID string, netcfg *cntr.NetworkConfig) error {
	if netcfg == nil {
		return fmt.Errorf("setup netns: nil network config")
	}

	if netcfg.HolderPid > 0 {
		if path, err := netns.RegisterExisting(sandboxID, netcfg.HolderPid); err == nil {
			netcfg.NetworkID = path
			netcfg.NetworkCreated = true
			return nil
		}
		log.Warnf("existing netns holder pid %d for sandbox %s is invalid; recreating", netcfg.HolderPid, sandboxID)
		netcfg.HolderPid = 0
	}

	pid, path, err := netns.Create(sandboxID)
	if err != nil {
		return err
	}

	netcfg.NetworkID = path
	netcfg.NetworkCreated = true
	netcfg.HolderPid = pid
	return nil
}

func cleanupNetNS(sandboxID string, netcfg *cntr.NetworkConfig) error {
	if netcfg == nil {
		return fmt.Errorf("cleanup netns: nil network config")
	}

	if err := netcfg.NetworkCleanup(sandboxID); err != nil {
		return err
	}
	return nil

}

// getConfigPathFromOldCRI extracts ConfigPath from legacy containerd CRI option structs
// without requiring optional build tags. When the incoming message exposes either a
// GetConfigPath method or a ConfigPath string field, the path is returned.
func getConfigPathFromOldCRI(v any) (string, bool) {
	if v == nil {
		return "", false
	}

	type configPathGetter interface {
		GetConfigPath() string
	}
	if getter, ok := v.(configPathGetter); ok {
		if path := getter.GetConfigPath(); path != "" {
			return path, true
		}
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return "", false
		}
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return "", false
	}

	field := rv.FieldByName("ConfigPath")
	if field.IsValid() && field.Kind() == reflect.String {
		if path := field.String(); path != "" {
			return path, true
		}
	}
	return "", false
}

func (s *shimService) getContainerStatus(id string) (task.Status, error) {
	if s.sandbox == nil {
		log.Debugf("Sandbox is nil, cannot get status for container %s", id)
		return task.Status_UNKNOWN, er.SandboxNotFound
	}
	cs, err := s.sandbox.StatusContainer(id)
	if err != nil {
		return task.Status_UNKNOWN, err
	}

	var st task.Status
	switch cs.State.State {
	case cntr.StateReady:
		st = task.Status_CREATED
	case cntr.StateRunning:
		st = task.Status_RUNNING
	case cntr.StatePaused:
		st = task.Status_PAUSED
	case cntr.StateStopped:
		st = task.Status_STOPPED
	}

	return st, nil
}
