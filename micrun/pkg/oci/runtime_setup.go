package oci

import (
	"encoding/json"
	"fmt"
	defs "micrun/definitions"
	log "micrun/logger"
	"micrun/pkg/pedestal"
	"micrun/pkg/utils"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// Configuration keys for runtime settings.
const (
	KeyStaticResource   = "static_resource"       // default=true
	KeyClientLimit      = "max_client_number"     // default=0, unlimited
	KeyLinuxContainer   = "enable_host_container" // default=false
	KeyDebug            = "debug"                 // default=false
	KeyStateDir         = "state_dir"             // default=defs.StateDir
	KeyPauseImg         = "pause_image"           // default=defs.PauseImage
	KeyMaxContainerVCPU = "max_container_vcpu"    // default=0, unlimited
	KeySandboxMinVCPU   = "sandbox_minimum_vcpu"  // default=1
	KeyHugePage         = "hugepage_enable"       // only for Xen; default=false
	KeyExclusiveDom0CPU = "exclusive_dom0_cpu"    // default=false, reserve Dom0 CPUs
	KeyMinMemory        = "container_minmem"      // default base memory for container
	KeyMaxMemory        = "container_maxmem"      // default max memory for container
	KeyDefaultFirmware  = "firmware_path"         // default firmware path when annotation not set
	KeySharedCPUPool    = "shared_cpu_pool"       // default=false, shared CPU pool for Xen cpupool management
)

// final fallbacks:
const defaultMaxContainerVCPUs = 8
const defaultContainerInitMemMiB = 32

var (
	HostPedType       = pedestal.GetHostPed()
	thredsholdMemHigh = pedestal.MemHighThreshold()
	thredsholdMemLow  = pedestal.MemLowThreshold()
	runtimeConfigKeys = []string{
		KeyStaticResource,
		KeyClientLimit,
		KeyDebug,
		KeyLinuxContainer,
		KeyStateDir,
		KeyPauseImg,
		KeyMaxContainerVCPU,
		KeySandboxMinVCPU,
		KeyHugePage,
		KeyExclusiveDom0CPU,
		KeyMaxMemory,
		KeyMinMemory,
		KeyDefaultFirmware,
		KeySharedCPUPool,
	}
)

type RuntimeConfig struct {
	Debug bool
	// TODO: enable Linux host act as a container
	HostLinuxContainer bool
	MaxClinetNum       uint32

	// Global resource management settings
	MaxContainerVCPUs uint32 // Maximum CPU cores visible for containers
	// NOTICE: MaxContainerMemMB is the initial memory threshold, not the init max available memory of RTOS
	MaxContainerMemMB uint32
	// Reservation memory for containers
	MinContainerMemMB        uint32
	HugePageSupport          bool
	StaticResourceManagement bool
	SharedCPUPool            bool // Shared CPU pool for Xen cpupool management

	// MICA-specific configurations
	ImagePath   string
	AuxFilePath string

	PauseImage          string
	MiniVCPUNum         uint32
	DefaultFirmwarePath string
	ExclusiveDom0CPU    bool
}

// NewRuntimeConfig returns a default RuntimeConfig.
func NewRuntimeConfig() *RuntimeConfig {
	ped := pedestal.GetHostPed()
	var staticResource bool
	if ped == pedestal.OpenAMP {
		staticResource = true
	}

	cfg := RuntimeConfig{
		StaticResourceManagement: staticResource,
		PauseImage:               defs.PauseImage,
		MinContainerMemMB:        32,
		MaxContainerVCPUs:        defaultMaxContainerVCPUs,
	}
	return &cfg
}

// ini conf
// TODO: with expanding of micran runtime config, we will migrate gookit.ini/v2 to
// out ParseConfigINI, ParseConfigINI requires only half memory of ini package and faster
// for large ini file parsing
func (r *RuntimeConfig) ParseRuntimeFromINI(configPath string) error {
	if _, err := os.Stat(configPath); err != nil {
		return err
	}
	filtered, err := utils.ParseINI(configPath, runtimeConfigKeys)
	if err != nil {
		return err
	}

	log.Debugf("parsed runtime config: %v", filtered)
	r.convertRawConfig(filtered)
	return nil
}

// TODO: finished
// use "github.com/BurntSushi/toml"
func (r *RuntimeConfig) ParseRuntimeFromToml(configPath string) error {
	if _, err := os.Stat(configPath); err != nil {
		return err
	}
	return fmt.Errorf("parse micrun config from toml is not supported yet")
}

// workaround, should be replaced
func (r *RuntimeConfig) convertRawConfig(raw map[string]string) {
	r.SetStaticResourceManagement(raw[KeyStaticResource])
	r.SetDebug(raw[KeyDebug])
	r.SetPauseImage(raw[KeyPauseImg])
	r.SetMaxContainerVCPUs(raw[KeyMaxContainerVCPU])
	r.SetMaxContainerMemMB(raw[KeyMaxMemory])
	r.SetMinContainerMemMB(raw[KeyMinMemory])
	r.SetMiniVCPUNum(raw[KeySandboxMinVCPU])
	r.SetHugePageSupport(raw[KeyHugePage])
	r.SetExclusiveDom0CPU(raw[KeyExclusiveDom0CPU])
	r.SetSharedCPUPool(raw[KeySharedCPUPool])
	r.SetStateDir(raw[KeyStateDir])
	r.SetDefaultFirmwarePath(raw[KeyDefaultFirmware])
}

func (r *RuntimeConfig) SetDebug(debugStr string) {
	debug, err := strconv.ParseBool(debugStr)
	if err != nil {
		log.Debugf("failed to parse debug value %v into bool: %v", debugStr, err)
		debug = false
	}
	r.Debug = debug
}

func (r *RuntimeConfig) SetMaxContainerVCPUs(cpuString string) {
	vcpu, err := strconv.ParseUint(cpuString, 10, 32)
	if err != nil {
		log.Debugf("failed to parse max container cpus %v into uint32", cpuString, err)
		r.MaxContainerVCPUs = defaultMaxContainerVCPUs
		return
	}
	if vcpu == 0 {
		log.Debugf("max container cpus parsed as 0, defaulting to %d", defaultMaxContainerVCPUs)
		r.MaxContainerVCPUs = defaultMaxContainerVCPUs
		return
	}
	r.MaxContainerVCPUs = uint32(vcpu)
}

func (r *RuntimeConfig) SetMaxContainerMemMB(memString string) {
	mem, err := strconv.ParseUint(memString, 10, 32)
	if err != nil || memoryOutOfRange(uint32(mem)) {
		log.Warnf("failed to parse max container memory %v into uint32 or out or range: %v", memString, err)
		r.MaxContainerMemMB = thredsholdMemHigh
		return
	}

	r.MaxContainerMemMB = uint32(mem)
}

func (r *RuntimeConfig) SetMinContainerMemMB(memString string) {
	mem, err := strconv.ParseUint(memString, 10, 32)
	if err != nil || memoryOutOfRange(uint32(mem)) {
		log.Debugf("failed to parse min container memory %v into uint32 or out or range", memString, err)
		r.MinContainerMemMB = thredsholdMemLow
		return
	}

	r.MinContainerMemMB = uint32(mem)
}

func (r *RuntimeConfig) SetHugePageSupport(hugePageStr string) {
	hugePage, err := strconv.ParseBool(hugePageStr)
	if err != nil {
		log.Debugf("failed to parse hugepage %v into bool", hugePageStr, err)
		hugePage = false
	}
	r.HugePageSupport = hugePage
}

func (r *RuntimeConfig) SetExclusiveDom0CPU(flag string) {
	if strings.TrimSpace(flag) == "" {
		return
	}
	enabled, err := strconv.ParseBool(flag)
	if err != nil {
		log.Debugf("failed to parse exclusive_dom0_cpu %q into bool", flag)
		return
	}
	r.ExclusiveDom0CPU = enabled
	pedestal.EnableDom0CPUExclusive(enabled)
}

func (r *RuntimeConfig) SetPauseImage(pauseImage string) {
	r.PauseImage = pauseImage
}

func (r *RuntimeConfig) SetDefaultFirmwarePath(path string) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return
	}
	r.DefaultFirmwarePath = trimmed
}

func (r *RuntimeConfig) SetStaticResourceManagement(staticResourceStr string) {
	staticResource, err := strconv.ParseBool(staticResourceStr)
	if err != nil {
		log.Debugf("failed to parse static_resource %v into bool", staticResourceStr, err)
		staticResource = false
	}
	r.StaticResourceManagement = staticResource
}

func (r *RuntimeConfig) SetMiniVCPUNum(miniVCPUString string) {
	miniVCPU, err := strconv.ParseUint(miniVCPUString, 10, 32)
	if err != nil {
		log.Debugf("failed to parse mini vcpu %v into uint32", miniVCPUString, err)
	}
	r.MiniVCPUNum = uint32(miniVCPU)
}

func (r *RuntimeConfig) SetClientLimit(clientLimitString string) {
	clientLimit, err := strconv.ParseUint(clientLimitString, 10, 32)
	if err != nil {
		log.Debugf("failed to parse client limit %v into uint32", clientLimitString, err)
	}
	r.MaxClinetNum = uint32(clientLimit)
}

func (r *RuntimeConfig) SetLinuxContainer(linuxContainerStr string) {
	linuxContainer, err := strconv.ParseBool(linuxContainerStr)
	if err != nil {
		log.Debugf("failed to parse linux container %v into bool", linuxContainerStr, err)
		linuxContainer = false
	}
	r.HostLinuxContainer = linuxContainer
}

func (r *RuntimeConfig) SetStateDir(stateDir string) {
	// Note: This field doesn't exist in RuntimeConfig yet, but the key is defined
	// For now, we'll just log it since it's a path configuration
	log.Debugf("setting state dir to: %v", stateDir)
}

func (r *RuntimeConfig) SetSharedCPUPool(sharedCPUPoolStr string) {
	if strings.TrimSpace(sharedCPUPoolStr) == "" {
		return
	}
	sharedCPUPool, err := strconv.ParseBool(sharedCPUPoolStr)
	if err != nil {
		log.Debugf("failed to parse shared_cpu_pool %q into bool", sharedCPUPoolStr)
		return
	}
	r.SharedCPUPool = sharedCPUPool
}

// ParseRuntimeConfigFromAnno parses runtime configuration from annotations.
// Annotations hold highest priority for values.
func (cfg *RuntimeConfig) ParseRuntimeConfigFromAnno(annotations map[string]string) *RuntimeConfig {
	// Parse runtime-level annotations with mica annotation prefix
	for key, value := range annotations {
		if !strings.HasPrefix(key, defs.MicrunAnnotationPrefix) || value == "" {
			continue
		}

		switch key {
		case defs.RuntimeDebug:
			cfg.SetDebug(value)
		case defs.RuntimePrefix + "max_container_cpus":
			cfg.SetMaxContainerVCPUs(value)
		case defs.RuntimePrefix + "max_container_memory":
			cfg.SetMaxContainerMemMB(value)
		case defs.RuntimePrefix + "cpu_scheduler_policy":
			log.Debugf("CPU scheduler policy not implemented, ignoring: %s", value)
		case defs.RuntimePrefix + "memory_overcommit":
			log.Debugf("memory overcommit not implemented, ignoring: %s", value)
		case defs.RuntimePrefix + "pause":
			cfg.SetPauseImage(value)
		case defs.RuntimeExclusiveDom0CPU:
			cfg.SetExclusiveDom0CPU(value)
		}
	}

	return cfg
}

func parseConfigJSON(file string) (specs.Spec, error) {
	configBytes, err := os.ReadFile(file)
	if err != nil {
		return specs.Spec{}, err
	}

	var config specs.Spec
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return specs.Spec{}, err
	}

	return config, nil
}

func LoadSpec(bundle string) (specs.Spec, error) {
	// For docker , config.v2.json, this line is useless;
	configPath := filepath.Join(bundle, "config.json")
	return parseConfigJSON(configPath)
}

// 2MB < cfgmem <
func memoryOutOfRange(cfgmem uint32) bool {
	if cfgmem > thredsholdMemHigh {
		log.Debugf("configurated micran memory out of range, set to %dMB by default", thredsholdMemHigh)
		return true
	}

	if cfgmem < thredsholdMemLow {
		log.Debugf("configurated micran memory out of range, set to %dMB by default", thredsholdMemLow)
		return true
	}

	return false

}
