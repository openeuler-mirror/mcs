package micantainer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	defs "micrun/definitions"
	er "micrun/errors"
	log "micrun/logger"
	"micrun/pkg/cpuset"
	"micrun/pkg/libmica"
	"micrun/pkg/utils"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/hashicorp/go-multierror"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

const (
	okCode = 0
)

// Define the structure that matches what we store
type SandboxStorage struct {
	ID      string        `json:"id"`
	State   SandboxState  `json:"state"`
	Config  SandboxConfig `json:"config"`
	Network NetworkConfig `json:"network"`
	// Containers map[string]*Container `json:"containers"`
}

// Status is a graph of the sanbox, contains more than state
type SandboxStatus struct {
	ContainersState []ContainerStatus
	Annotations     map[string]string
	ID              string
	State           SandboxState
}

// expand fields of sandboxconfigs as sandbox memebers
type Sandbox struct {
	ctx context.Context
	// use annoymous field to avoid unused fields wanring
	sync.Mutex
	// fs, storage, devices, volumes...
	// monitor
	resManager SandboxResource
	config     *SandboxConfig
	containers map[string]*Container
	id         string
	network    Network
	state      SandboxState

	vcpuAlreadyPinned bool

	annotaLock *sync.RWMutex
	wg         *sync.WaitGroup
}

// impl SandboxTraits for Sandbox
func (s *Sandbox) GetAllContainers() []ContainerTraits {
	list := make([]ContainerTraits, 0, len(s.containers))
	for _, c := range s.containers {
		list = append(list, c)
	}
	return list
}

func (s *Sandbox) SandboxID() string {
	return s.id
}

func (s *Sandbox) Annotation(key string) (string, error) {
	s.annotaLock.RLock()
	defer s.annotaLock.RUnlock()
	value, found := s.config.Annotations[key]
	if !found {
		return "", fmt.Errorf("annotation not found: %s", key)
	}
	return value, nil
}

// TALK: diffult to do it?
func (s *Sandbox) Monitor() {
}

func (s *Sandbox) GetNetNamespace() string {
	return s.network.NetID()
}

func (s *Sandbox) NetnsHolderPID() int {
	if cfg := s.config; cfg != nil {
		return cfg.NetworkConfig.HolderPid
	}
	return 0
}

func (s *Sandbox) Start(ctx context.Context) error {
	cur := s.state.State
	log.Debugf("current sandbox state=%s", cur)

	//  If restored as 'creating', normalize to 'ready' before starting
	if cur == StateCreating {
		if err := s.setSandboxState(StateReady); err != nil {
			return err
		}
		cur = s.state.State
	}

	// If already running, ensure all containers are running
	if cur == StateRunning {
		log.Debugf("sandbox %s already running, checking containers", s.id)
		for _, c := range s.containers {
			if c.checkState() != StateRunning {
				if err := c.start(ctx); err != nil {
					return err
				}
			}
		}
		if err := s.StoreSandbox(ctx); err != nil {
			return err
		}
		return nil
	}

	if err := s.state.Transition(cur, StateRunning); err != nil {
		log.Debugf("transition error: from=%s to=%s", cur, StateRunning)
		return err
	}

	oldState := cur
	if err := s.setSandboxState(StateRunning); err != nil {
		return fmt.Errorf("set Sandbox state error: %v", err)
	}
	log.Debugf("sandbox state: %s -> %s", oldState, s.state.State)

	var startErr error
	defer func() {
		if startErr != nil {
			s.setSandboxState(oldState)
		}
	}()

	for _, c := range s.containers {
		if startErr = c.start(ctx); startErr != nil {
			return startErr
		}
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return err
	}

	return nil

}

// Stop stops all containers inside the sandbox as well as sandbox itself
// For a forced stopping,  ignore container stop failures
func (s *Sandbox) Stop(ctx context.Context, force bool) error {

	if s.state.State == StateStopped {
		return nil
	}

	if err := s.state.Transition(s.state.State, StateStopped); err != nil {
		return err
	}

	for _, c := range s.containers {
		if err := c.stop(ctx, force); err != nil {
			return err
		}
	}

	if err := s.stopClients(ctx); err != nil && !force {
		return err
	}

	log.Debug("stop monitor and console")

	if err := s.setSandboxState(StateStopped); err != nil {
		return err
	}

	if err := s.removeNetwork(); err != nil && !force {
		return err
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return err
	}

	return nil
}

// Delete delete containers and then clean storage
func (s *Sandbox) Delete(ctx context.Context) error {

	if s.state.State != StateReady &&
		s.state.State != StatePaused &&
		s.state.State != StateStopped {
		return fmt.Errorf("sandbox is not ready, paused, or stopped, cannot delete")
	}

	for _, c := range s.containers {
		if err := c.delete(ctx); err != nil {
			log.Errorf("failed to delete container %s", c.id)
		}
	}

	if err := s.removeNetwork(); err != nil {
		log.Warnf("failed to remove network for sandbox %s: %v", s.id, err)
	}

	return s.cleanSandboxStorage()

}

// CreateContainer creates a new container in the sandbox
// This should be called only when the sandbox is already created.
// It will add new container config to sandbox.config.Containers
func (s *Sandbox) CreateContainer(ctx context.Context, config ContainerConfig) (ContainerTraits, error) {

	id := config.ID
	if _, ok := s.containers[id]; ok {
		log.Errorf("container %s already exists", id)
		return nil, er.AlreadyExists
	}
	s.config.ContainerConfigs[id] = &config
	if s.config.InfraOnly && !config.IsInfra {
		s.config.InfraOnly = false
	}
	newc := s.config.ContainerConfigs[id]

	var err error
	defer func() {
		if err != nil {
			if len(s.config.ContainerConfigs) > 0 {
				delete(s.config.ContainerConfigs, id)
			}
		}
	}()

	c, err := newContainer(ctx, s, newc)
	if err != nil {
		return nil, err
	}

	// Validate the container after creation but before starting
	if !c.validMicaContainer() {
		return nil, fmt.Errorf("invalid mica container: %v", c)
	}

	if err = c.create(ctx); err != nil {
		return nil, err
	}

	if err = s.addContainer(c); err != nil {
		return nil, err

	}
	defer func() {
		if err == nil {
			return
		}

		log.Errorf("failed to create container %s: %v", id, err)

		if errStop := c.stop(ctx, true); errStop != nil {
			log.Errorf("failed to stop container %s after creation failure: %v", id, errStop)
		}
		log.Debug("remove stopped container from sandbox")
		s.removeContainer(c.id)
	}()

	if err = s.checkVCPUsPinning(ctx); err != nil {
		return nil, err
	}

	// update sandbox status
	if err = s.StoreSandbox(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Sandbox) removeContainer(containerID string) error {
	log.Debugf("remove container %s", containerID)
	if s == nil {
		return fmt.Errorf("sandbox is nil")
	}

	if containerID == "" {
		return er.EmptyContainerID
	}

	if _, ok := s.containers[containerID]; !ok {
		return errors.Wrapf(er.ContainerNotFound, "Could not remove the container %q from the sandbox %q containers list",
			containerID, s.id)
	}

	delete(s.containers, containerID)
	return nil
}

func (s *Sandbox) DeleteContainer(ctx context.Context, id string) (ContainerTraits, error) {
	log.Debugf("delete container %s from sandbox", id)
	if s == nil {
		return nil, er.SandboxNotFound
	}
	if id == "" {
		return nil, er.EmptyContainerID
	}

	c, ok := s.containers[id]
	if !ok {
		return nil, er.ContainerNotFound
	}

	if err := c.delete(ctx); err != nil {
		return nil, err
	}

	// Guard nil config; delete from container configs if present
	if s.config != nil {
		delete(s.config.ContainerConfigs, id)
	}

	// Clean resManager per-container mirrors if present
	if s.resManager.ContainerCpuSets != nil {
		delete(s.resManager.ContainerCpuSets, id)
	}
	if s.resManager.ContainerVcpus != nil {
		delete(s.resManager.ContainerVcpus, id)
	}

	// Explicitly refresh aggregated resources; debounce/logging inside updateResources
	if err := s.updateResources(ctx); err != nil {
		log.Debugf("ignore updateResources error after delete %s: %v", id, err)
	}

	if err := s.checkVCPUsPinning(ctx); err != nil {
		return nil, err
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Sandbox) StartContainer(ctx context.Context, id string) (ContainerTraits, error) {
	c, ok := s.containers[id]
	if !ok {
		return nil, er.ContainerNotFound
	}

	// start client os, os start the task from entry inside the OS image
	if err := c.start(ctx); err != nil {
		return nil, err
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return nil, err
	}

	if err := s.checkVCPUsPinning(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Sandbox) StopContainer(ctx context.Context, id string, force bool) (ContainerTraits, error) {
	c, ok := s.containers[id]
	if !ok {
		return nil, er.ContainerNotFound
	}
	if err := c.stop(ctx, force); err != nil {
		return nil, err
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// Stop the container forcely and pop it.
func (s *Sandbox) KillContainer(ctx context.Context, id string) (ContainerTraits, error) {
	c, ok := s.containers[id]
	if !ok {
		return nil, er.ContainerNotFound
	}

	if libmica.ClientNotExist(c.id) {
		return c, nil
	}

	if err := c.kill(); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Sandbox) StatusContainer(id string) (ContainerStatus, error) {
	cs := ContainerStatus{}
	if id == "" {
		log.Debugf("status container: empty id")
		return cs, er.EmptyContainerID
	}

	if c, ok := s.containers[id]; ok {
		if _, err := c.ensureClientPresence(); err != nil {
			return cs, err
		}

		if c.checkState() == StateDown {
			return cs, er.ContainerNotFound
		}

		rootfs := c.config.Rootfs.Source
		if c.config.Rootfs.Mounted {
			rootfs = c.config.Rootfs.Target
		}

		// TODO: no need to store starttime in taskinfo, collapsing is unneeded
		cs.Spec = nil
		cs.State = c.state
		cs.ID = c.id
		cs.Rootfs = rootfs
		cs.Pid = c.GetPid()
		cs.Annotations = c.config.Annotations
		return cs, nil
	}
	log.Debugf("container %s not found in sandbox %s", id, s.id)
	return cs, nil
}

func (s *Sandbox) StatsContainer(ctx context.Context, id string) (ContainerStats, error) {
	c, ok := s.containers[id]
	if !ok {
		return ContainerStats{}, er.ContainerNotFound
	}

	stats, err := c.stats()
	if err != nil {
		log.Errorf("failed to get stats for container %s: %v", id, err)
		return ContainerStats{}, err
	}
	return *stats, nil
}

func (s *Sandbox) IOStream(containerID, taskID string) (io.WriteCloser, io.Reader, io.Reader, error) {
	if s.state.State != StateRunning {
		return nil, nil, nil, er.SandboxDown
	}

	c, ok := s.containers[containerID]
	if !ok {
		return nil, nil, nil, er.ContainerNotFound
	}

	return c.ioStream(taskID)
}

// Not supported well
// TODO: aftet integrate micrun into micad, we can achive sending signals to RTOS clients
// return int perform as an exit code placeholder for now
// NOTICE: container : task : RTOS Client = 1 : 1 : 1
func (s *Sandbox) WaitContainerExit(ctx context.Context, containerID string) (int32, error) {

	c, ok := s.containers[containerID]
	if !ok {
		return okCode, er.ContainerNotFound
	}

	state := c.checkState()
	if state == StateDown {
		return okCode, er.ContainerDown
	}

	// RISK: we set sandbox state to stop before applying container stop
	if state == StateStopped {
		return okCode, nil
	}

	log.Infof("wait for container=%s exiting", containerID)

	notifier := c.exitNotifierForState(state)

	select {
	case <-ctx.Done():
		return okCode, ctx.Err()
	case <-notifier:
		return okCode, nil
	}
}

func (s *Sandbox) WinResize(ctx context.Context, containerID string, height, width uint32) error {
	if s.state.State != StateRunning {
		return er.SandboxDown
	}

	c, ok := s.containers[containerID]
	if c == nil || !ok {
		return er.ContainerNotFound
	}

	return c.winresize(height, width)
}

func (s *Sandbox) PauseContainer(ctx context.Context, id string) error {

	c, ok := s.containers[id]
	if !ok {
		return er.ContainerNotFound
	}

	if err := c.pause(ctx); err != nil {
		return err
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return err
	}

	return nil
}

func (s *Sandbox) ResumeContainer(ctx context.Context, id string) error {
	c, ok := s.containers[id]
	if !ok {
		return er.ContainerNotFound
	}

	if err := c.resume(ctx); err != nil {
		return err
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return err
	}

	return nil
}

func (s *Sandbox) UpdateContainer(ctx context.Context, id string, resources specs.LinuxResources) error {
	log.Debugf("UpdateContainer: container=%s, resources=%+v", id, resources)

	if s.config.StaticResourceMgmt {
		log.Debugf("UpdateContainer ignored in static resource management mode")
		return nil
	}

	c, ok := s.containers[id]
	if !ok {
		return er.ContainerNotFound
	}

	if err := c.update(ctx, resources); err != nil {
		log.Debugf("UpdateContainer best-effort ignore c.update error for %s: %v", id, err)
	}

	if err := s.checkVCPUsPinning(ctx); err != nil {
		log.Debugf("UpdateContainer best-effort ignore checkVCPUsPinning error: %v", err)
	}

	if err := s.StoreSandbox(ctx); err != nil {
		log.Debugf("UpdateContainer best-effort ignore StoreSandbox error: %v", err)
	}

	return nil
}

// privates:
func (s *Sandbox) setSandboxState(state StateString) error {
	if state == "" {
		return er.InvalidState
	}
	s.state.State = state
	return nil
}

// store sandbox information to disk
func (s *Sandbox) StoreSandbox(ctx context.Context) error {
	target, err := s.newSandboxStoragePath()
	if err != nil {
		return err
	}

	// Create serializable representation of sandbox
	serializable := SandboxStorage{
		ID:     s.id,
		State:  s.state,
		Config: *s.config,
	}

	// NOTICE: remove unnecessary runtime reflection, make codes clean and faster
	switch netCfg := s.network.(type) {
	case *NetworkConfig:
		serializable.Network = *netCfg
	case *DummyNetwork:
		serializable.Network = NetworkConfig{
			NetworkID:      netCfg.NetID(),
			NetworkCreated: netCfg.NetworkIsCreated(),
		}
	default:
		if s.config != nil {
			serializable.Network = s.config.NetworkConfig
		}
	}

	if err := utils.SaveStructToJSON(target, serializable); err != nil {
		return err
	}
	return nil
}

func (s *Sandbox) sandboxStoragePath() string {
	return filepath.Join(defs.SandboxDataDir, s.id)
}

func (s *Sandbox) newSandboxStoragePath() (string, error) {
	dir := s.sandboxStoragePath()
	if err := os.MkdirAll(dir, defs.DirMode); err != nil {
		return "", err
	}
	stateFile := filepath.Join(dir, defs.SandboxStateFile)
	return stateFile, nil
}

func (s *Sandbox) cleanSandboxStorage() error {
	if s.id == "" {
		return er.EmptySandboxID
	}
	dir := s.sandboxStoragePath()
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return nil

}

func (s *Sandbox) addContainer(c *Container) error {

	if _, ok := s.containers[c.id]; ok {
		return er.DuplicatedKey
	}
	s.containers[c.id] = c
	return nil
}

// TODO: not finished well
// NOTICE: we need idempotence, and make removeNetwork()
func (s *Sandbox) removeNetwork() error {
	log.Infof("remove network for sandbox %s", s.id)
	if s.config == nil {
		return nil
	}

	if err := s.config.NetworkConfig.NetworkCleanup(s.id); err != nil {
		return err
	}
	return nil
}

func (s *Sandbox) stopClients(ctx context.Context) error {
	log.Infof("stopping client os in sandbox %s", s.id)
	for _, c := range s.containers {
		if err := c.stop(ctx, true); err != nil {
			log.Errorf("failed to stop container %s: %v", c.id, err)
			return err
		}
	}
	return nil
}

// setup sandbox
func CreateSandbox(ctx context.Context, cfg *SandboxConfig) (*Sandbox, error) {
	s, err := createSandboxFromConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	return s, nil
}

// TODO: dirty initialization! refactor it
func newSandbox(ctx context.Context, config SandboxConfig) (sb *Sandbox, retErr error) {
	if !config.valid() {
		return nil, fmt.Errorf("invalid sandbox configuration")
	}
	s := &Sandbox{
		ctx:        ctx,
		config:     &config,
		containers: make(map[string]*Container),
		id:         config.ID,
		state: SandboxState{
			State:   StateCreating,
			Ped:     HostPedType.String(),
			Version: defs.SandboxVersion,
		},
		resManager: *NewResMgmt(),
		wg:         &sync.WaitGroup{},
		annotaLock: &sync.RWMutex{},
	}

	if s.config != nil {
		s.network = &s.config.NetworkConfig
	} else {
		s.network = &DummyNetwork{}
	}

	if err := s.restore(); err != nil {
		log.Debugf("failed to restore sandbox %s: %v", s.id, err)
	}
	return s, nil
}

func createSandbox(ctx context.Context, config *SandboxConfig) (*Sandbox, error) {

	s, err := newSandbox(ctx, *config)
	if err != nil {
		return nil, err
	}

	if s.state.State == StateReady || s.state.State == StateRunning {
		log.Debugf("sandbox already in ready/running state, creation finished.")
		return s, nil
	}

	hostname := s.config.Hostname
	if len(hostname) > maxHostnameLength {
		hostname = hostname[:maxHostnameLength]
	}
	s.config.Hostname = hostname

	if err := s.setSandboxState(StateReady); err != nil {
		return nil, err
	}

	return s, nil
}

// createSandboxFromConfig creates a new sandbox instance from sandbox config
// 1. createSandboxFromConfig instance, and setup
// 2. cleanup if error happens
func createSandboxFromConfig(ctx context.Context, config *SandboxConfig) (_ *Sandbox, err error) {
	s, err := createSandbox(ctx, config)

	defer func() {
		if err != nil {
			log.Debugf("hooked delete sandbox!")
			s.Delete(ctx)
		}
	}()

	if err = s.createNetwork(ctx); err != nil {
		log.Debugf("failed to create network: %v", err)
		return nil, err
	}

	defer func() {
		if err != nil {
			s.removeNetwork()
		}
	}()

	s.postNetworkCreated()

	if err = s.initContainers(ctx); err != nil {
		log.Debugf("failed to init containers: %v", err)
		return nil, err
	}

	return s, nil
}

func (s *Sandbox) restore() error {
	ss, err := restoreSandbox(s.ctx, s.id)

	if err != nil {
		log.Warnf("failed to restore sandbox state: %v", err)
		return nil
	}

	if ss != nil {
		if ss.ID != s.id {
			log.Debugf("sandbox ID mismatch: %v != %v", ss.ID, s.id)
			log.Pretty("%v", ss)
			return fmt.Errorf("sandbox ID mismatch: %v != %v", ss.ID, s.id)
		}

		s.state.Ped = ss.State.Ped
		s.state.Version = ss.State.Version
		s.state.State = ss.State.State
		s.config = &ss.Config
		if s.config != nil {
			s.config.NetworkConfig = ss.Network
			s.network = &s.config.NetworkConfig
		} else {
			s.network = &DummyNetwork{}
		}
	}

	return nil
}

// restoreSandbox loads an existing sandbox from storage, by sandbox id
func restoreSandbox(ctx context.Context, id string) (*SandboxStorage, error) {
	// Load sandbox configuration from storage
	sandboxDir := filepath.Join(defs.SandboxDataDir, id)
	configPath := filepath.Join(sandboxDir, defs.SandboxStateFile)

	raw, err := utils.RestoreStructFromJSON(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Debugf("not found sandbox state file: %s, sandbox may have been already cleaned up", configPath)
			return nil, er.SandboxNotFound
		}
		return nil, fmt.Errorf("failed to load sandbox state from %s: %w", configPath, err)
	}

	// Convert to JSON and then unmarshal into our struct
	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal raw data: %w", err)
	}

	var storage SandboxStorage
	if err := json.Unmarshal(jsonBytes, &storage); err != nil {
		return nil, fmt.Errorf("failed to unmarshal sandbox storage: %w", err)
	}

	// Return just the state part as requested by the function signature
	return &storage, nil
}

// sandbox is not ready for being operated
func (s *Sandbox) notOperational() bool {
	return s.state.State != StateReady && s.state.State != StateRunning
}

func (s *Sandbox) createNetwork(ctx context.Context) error {
	log.Debugf("createNetwork.")
	return nil
}

func (s *Sandbox) postNetworkCreated() error {
	if netConfig, ok := s.network.(*NetworkConfig); ok {
		return netConfig.postCreated()
	}
	return nil
}

// Add containers (new or restored) to sandbox
func (s *Sandbox) initContainers(ctx context.Context) error {
	for _, cc := range s.config.ContainerConfigs {
		if s.config.InfraOnly && cc != nil && !cc.IsInfra {
			s.config.InfraOnly = false
		}
		c, err := newContainer(ctx, s, cc)
		if err != nil {
			return err
		}

		// Validate the container after creation but before starting
		if !c.validMicaContainer() {
			return fmt.Errorf("invalid mica container: %v", c)
		}

		if err := c.create(ctx); err != nil {
			return err
		}
		if err := s.addContainer(c); err != nil {
			return err
		}
	}

	if err := s.updateResources(ctx); err != nil {
		return err
	}

	if err := s.checkVCPUsPinning(ctx); err != nil {
		return err
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return err
	}

	return nil
}

// TODO: universal pinning policy for different pedestals in Libmica.
// Pinning currently just constrains each container's VCPUs to the cpus derived
// from OCI cpusets: either all containers share the merged pool (SharedCPUPool)
// or each container uses its own cpuset.
// Without pinning we skip affinity
// updates entirely and let the pedestal schedule VCPUs freely.
// NOTICE: we do not "bind a vcpu" to a pcpu, instead we just set "vcpu set" to a pcpu set if pinning
func (s *Sandbox) checkVCPUsPinning(ctx context.Context) error {
	if s.config == nil {
		return fmt.Errorf("no sandbox config found")
	}

	if !s.config.EnableVCPUsPinning {
		return nil
	}

	cpus, _, err := s.getSandboxCpusetStr()
	if err != nil {
		return fmt.Errorf("failed to get CPUSet string: %v", err)
	}

	cpuSet, err := cpuset.Parse(cpus)
	if err != nil {
		return fmt.Errorf("failed to parse CPUSet string %s: %v", cpus, err)
	}
	cpuList := cpuSet.ToSlice()

	match := true

	if valid, outOfRangeCPUs := CpusetRangeValid(cpuList); !valid {
		match = false
		log.Debugf("these cpus are out of range: %v", outOfRangeCPUs)
		// TODO: handle the overrange cpus
	}

	// Only enforce CPU count equality in shared CPU pool mode
	if s.config.SharedCPUPool {
		numVCPUs, numCPUs := int(s.resManager.VcpuNum), len(cpuList)
		if numCPUs != numVCPUs {
			match = false
			log.Debugf("the number of cpusets %d is not equal to the number of vcpus %d", numCPUs, numVCPUs)
		}
	}

	if !match {
		if s.vcpuAlreadyPinned {
			s.vcpuAlreadyPinned = false
			log.Debugf("the sandbox is already pinned to cpusets")
		}
	}

	if err := s.pinVCPU(cpuSet); err != nil {
		log.Warnf("failed to pin vcpu: %v", err)
		return err
	}

	s.vcpuAlreadyPinned = true
	return nil
}

// merge all cpusets of containers in the sandbox
// ocispec load memory nodes part and cpus part of cpuset in field of Linux.Resource.CPU.{Mems, Cpus}
// string format cpuset is expected by mica
// memResult is unused
func (s *Sandbox) getSandboxCpusetStr() (string, string, error) {
	if s.config == nil {
		return "", "", nil
	}

	cpuResult := cpuset.NewCPUSet()
	memResult := cpuset.NewCPUSet()
	for _, cfg := range s.config.ContainerConfigs {
		if cfg.IsInfra {
			continue
		}
		resource := cfg.Resources
		if resource != nil {
			if resource.CPU == nil {
				continue
			}
			cpuStr := strings.TrimSpace(resource.CPU.Cpus)
			if cpuStr == "" {
				continue
			}
			currCPUSet, err := cpuset.Parse(cpuStr)
			if err != nil {
				return "", "", fmt.Errorf("unable to parse CPUset.cpus for container %s: %v", cfg.ID, err)
			}
			cpuResult = cpuResult.Union(currCPUSet)

			memStr := strings.TrimSpace(resource.CPU.Mems)
			if memStr == "" {
				continue
			}
			currMemSet, err := cpuset.Parse(memStr)
			if err != nil {
				return "", "", fmt.Errorf("unable to parse CPUset.mems for container %s: %v", cfg.ID, err)
			}
			memResult = memResult.Union(currMemSet)
		}
	}

	return cpuResult.String(), memResult.String(), nil
}

// recalculate resources pool for clients and call pedestal to resize
func (s *Sandbox) updateResources(ctx context.Context) error {
	if s == nil {
		return er.SandboxNotFound
	}

	if s.config == nil {
		return fmt.Errorf("sandbox config is nil")
	}

	if s.config.InfraOnly {
		return nil
	}

	if s.config.StaticResourceMgmt {
		log.Debug("static resource management is enabled, updating resource is not supported")
		return nil
	}

	sandboxVCPUs, err := calculateSandboxVCPUs(s)
	if err != nil {
		return err
	}

	sandboxVCPUs += s.config.PedConfig.MiniVCPUNum

	newSandboxMemoryMB := calculateSandboxMemory(s)

	oldVCPUs, newVCPUs := s.resManager.resizeVCPUs(sandboxVCPUs)
	if oldVCPUs != newVCPUs {
		log.Infof("sandbox total vcpu number from %d to %d", oldVCPUs, newVCPUs)
	}

	oldMemBytes, newMemBytes := s.resManager.resizeMemory(newSandboxMemoryMB)
	if oldMemBytes != newMemBytes {
		log.Infof("sandbox total memory usage from %d MiB to %d MiB", oldMemBytes>>20, newMemBytes>>20)
	}

	return nil
}

// creates new container instances in sandbox
func (s *Sandbox) loadContainersToSandbox(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("loadContainersToSandbox: is only called an existing sandbox")
	}

	for _, cc := range s.config.ContainerConfigs {
		c, err := newContainer(ctx, s, cc)
		if err != nil {
			return err
		}

		if err := s.addContainer(c); err != nil {
			return err
		}
	}

	return nil

}

// update cpu affinity for sandbox vcpu
// repin vcpus in vcpuList to the cpupool
func (s *Sandbox) pinVCPU(cpuSet cpuset.CPUSet) error {
	var result *multierror.Error

	if s.config.SharedCPUPool {
		// Shared CPU pool mode: pin all containers to the same union CPU set
		pcpuList := cpuSet.ToSlice()
		for cid, c := range s.containers {
			log.Infof("try to pin container %s vcpu affinity to shared cpuset %v", cid, pcpuList)
			if err := c.setVcpuAffinity(cpuSet); err != nil {
				result = multierror.Append(result, err)
			} else {
				s.resManager.ContainerCpuSets[cid] = cpuSet
			}
		}

		ret := result.ErrorOrNil()
		if ret == nil {
			// Keep sandbox VCPU statistic in sync with containers' vCPUs.
			if total, err := calculateSandboxVCPUs(s); err == nil {
				s.resManager.VcpuNum = total
			} else {
				s.resManager.VcpuNum = uint32(cpuSet.Size())
			}
			s.resManager.setNewPCpuList(pcpuList)
		}
		return ret
	}

	// Independent CPU pinning mode: each container uses its own cpuset
	allContainerCPUs := cpuset.NewCPUSet()
	for cid, c := range s.containers {
		var containerCPUSet cpuset.CPUSet
		if c.config != nil && c.config.Resources != nil && c.config.Resources.CPU != nil && c.config.Resources.CPU.Cpus != "" {
			parsed, err := cpuset.Parse(c.config.Resources.CPU.Cpus)
			if err != nil {
				result = multierror.Append(result, fmt.Errorf("failed to parse cpuset for container %s: %v", cid, err))
				continue
			}
			containerCPUSet = parsed
		} else {
			// No cpuset specified, skip pinning for this container
			log.Debugf("container %s has no cpuset specified, skipping CPU pinning", cid)
			continue
		}

		log.Debugf("try to pin container %s vcpu affinity to its own cpuset %v", cid, containerCPUSet.ToSlice())
		if err := c.setVcpuAffinity(containerCPUSet); err != nil {
			result = multierror.Append(result, err)
		} else {
			s.resManager.ContainerCpuSets[cid] = containerCPUSet
			allContainerCPUs = allContainerCPUs.Union(containerCPUSet)
		}
	}

	ret := result.ErrorOrNil()
	if ret == nil {
		// Keep sandbox VCPU statistic in sync with containers' vCPUs.
		if total, err := calculateSandboxVCPUs(s); err == nil {
			s.resManager.VcpuNum = total
		} else {
			s.resManager.VcpuNum = uint32(allContainerCPUs.Size())
		}
		s.resManager.setNewPCpuList(allContainerCPUs.ToSlice())
	}
	return ret
}
