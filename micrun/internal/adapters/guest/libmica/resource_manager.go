package libmica

import (
	"context"
	"fmt"
	"micrun/internal/ports"
	"micrun/internal/support/contextx"
	"micrun/internal/support/cpuset"
	log "micrun/internal/support/logger"
	"strconv"
	"strings"
)

var _ ports.GuestExecutor = (*MicaExecutor)(nil)

// MemoryThresholdMB returns the current memory threshold in MiB.
// 内存资源映射规范：
// 1. Container memory limit -> RTOS Client memory limit
// 2. Container memory reservation -> RTOS Client memory min
// 3. memoryThreshold 仅在 micaexecutor 中记录，保证 memory threshold >= container memory limit
func (me *MicaExecutor) MemoryThresholdMB() uint32 {
	me.mu.RLock()
	defer me.mu.RUnlock()
	return me.memoryThresholdMB
}

// CurrentMaxMem returns the current memory limit in MiB.
// 对应 RTOS Client memory limit，来自 Container memory limit
func (me *MicaExecutor) CurrentMaxMem() uint32 {
	me.mu.RLock()
	defer me.mu.RUnlock()
	if me.records.memoryMB == 0 {
		return 0
	}
	return me.records.memoryMB
}

// RecordMemoryState records the current memory state.
// memoryThreshold 设计为单调递增的，仅在新的 memory threshold 出现时才会正向更新
func (me *MicaExecutor) RecordMemoryState(current, threshold uint32) {
	me.mu.Lock()
	defer me.mu.Unlock()
	me.records.memoryMB = current
	if threshold == 0 {
		threshold = current
	}
	// 单调递增：只更新更大的阈值
	me.memoryThresholdMB = max(me.memoryThresholdMB, threshold)
}

// EnsureMemoryLimit applies the requested memory limit, expanding the pedestal maximum first when needed.
// 内存资源映射规范：
// 1. 保证 memory threshold >= container memory limit
// 2. memoryThreshold 单调递增，只增不减
// 3. 先更新 threshold，再更新实际内存限制
func (me *MicaExecutor) EnsureMemoryLimit(ctx context.Context, target uint32) error {
	current := me.CurrentMaxMem()
	threshold := me.MemoryThresholdMB()

	if threshold == 0 {
		threshold = current
	}

	// 保证 memory threshold >= container memory limit.
	// UpdateMemoryThreshold already sets memoryThresholdMB = max(target, ...)
	// under the lock, so no redundant write is needed here.
	if threshold < target {
		if err := me.UpdateMemoryThreshold(ctx, target); err != nil {
			return err
		}
	}

	if current == target {
		return nil
	}

	if err := me.UpdateMemory(ctx, target); err != nil {
		return err
	}

	return nil
}

func (me *MicaExecutor) updateResource(ctx context.Context, field MicaUpdateField, value string) error {
	// micad reads control messages into a fixed CTRL_MSG_SIZE (32) buffer
	// ("set <key> <value>") and copies them with strlcpy(…, 32), which keeps
	// only 31 bytes plus NUL: a message of exactly 32 bytes silently loses
	// its last character — a truncated cpuset that parses as a valid prefix
	// would bind the guest to a wrong CPU subset with no error. Reject
	// values whose message would reach that boundary.
	budget := micaCtrlMsgSize - len("set ") - len(string(field)) - 2
	if len(value) > budget {
		return fmt.Errorf("update value %q for %s exceeds micad control message budget (%d bytes)", value, field, budget)
	}
	req := MicaUpdateRequest{Field: field, Value: value}
	return me.micaCtl(ctx, MUpdate, me.ID, req.WireFormat())
}

func resourceUint(value uint32) string {
	return strconv.FormatUint(uint64(value), 10)
}

// number of visible vcpus
func (me *MicaExecutor) UpdateVCPUNum(ctx context.Context, newVCPUs uint32) (oldCPUs, newCPUs uint32, retErr error) {
	ctx = contextx.OrBackground(ctx)
	// Hold the write lock across both updateResource and cache update so
	// concurrent updates serialize: the cache write order matches the micad
	// application order, preventing TOCTOU desync between cache and hypervisor.
	me.mu.Lock()
	defer me.mu.Unlock()
	old := me.records.vcpuNum
	if me.Hypervisor != nil {
		maxCPUs := me.Hypervisor.MaxCPUNum(ctx)
		if newVCPUs > maxCPUs {
			return old, old, fmt.Errorf("vcpu request %d exceeds host maximum %d for %s", newVCPUs, maxCPUs, me.ID)
		}
	}
	log.Debugf("update vcpu num: container=%s old=%d new=%d", me.ID, old, newVCPUs)
	if err := me.updateResource(ctx, MicaUpdateVCPU, resourceUint(newVCPUs)); err != nil {
		log.Warnf("failed to update vcpu number: %v", err)
		return old, old, err
	}
	me.records.vcpuNum = newVCPUs
	return old, newVCPUs, nil
}

// RecordVCPUCount records the vCPU count after an out-of-band change (e.g. an
// xl fallback in applyInitialCPUSettings) so subsequent resource comparisons
// (NeedUpdateVCPUs/ReadResource) reflect the actual hypervisor state.
func (me *MicaExecutor) RecordVCPUCount(vcpus uint32) {
	me.mu.Lock()
	me.records.vcpuNum = vcpus
	me.mu.Unlock()
}

// UpdatePCPUConstraints binds a physical CPU set to a container.
func (me *MicaExecutor) UpdatePCPUConstraints(ctx context.Context, cpus string) error {
	me.mu.Lock()
	defer me.mu.Unlock()
	log.Debugf("update pcpu constraints: container=%s cpuset=%s", me.ID, cpus)
	if err := me.updateResource(ctx, MicaUpdatePCPUConstraints, cpus); err != nil {
		log.Warnf("failed to bind physical cpuset \"%s\" to container: %v", cpus, err)
		return err
	}
	me.records.cpuStr = [MaxCPUStringLen]byte{}
	copy(me.records.cpuStr[:], []byte(cpus))
	log.Debugf("updated to new cpuset: %s", cpus)
	return nil
}

func (me *MicaExecutor) UpdateCPUCapacity(ctx context.Context, cap uint32) error {
	me.mu.Lock()
	defer me.mu.Unlock()
	log.Debugf("update cpu capacity: container=%s old=%d new=%d", me.ID, me.records.cpuCapacity, cap)
	if err := me.updateResource(ctx, MicaUpdateCPUCapacity, resourceUint(cap)); err != nil {
		log.Warnf("failed to update cap time to %d that container can run: %v", cap, err)
		return err
	}
	me.records.cpuCapacity = cap
	log.Debugf("updated to new cpu capacity: %d", cap)
	return nil
}

func (me *MicaExecutor) UpdateCPUWeight(ctx context.Context, weight uint32) error {
	me.mu.Lock()
	defer me.mu.Unlock()
	log.Debugf("update cpu weight: container=%s old=%d new=%d", me.ID, me.records.cpuWeight, weight)
	if err := me.updateResource(ctx, MicaUpdateCPUWeight, resourceUint(weight)); err != nil {
		log.Warnf("failed to update cpu share time to %d that container can run: %v", weight, err)
		return err
	}
	me.records.cpuWeight = weight
	log.Debugf("updated to new cpu weight: %d", weight)
	return nil
}

// UpdateMemoryThreshold updates the memory threshold for the RTOS client.
// NOTICE: MemoryThreshold is not the max memory of client. It is the max memory
// that pedestal can allocate to container (pedestal max memory).
// Memory is just the max memory of a client.
// 内存资源映射规范：
// 1. memoryThreshold 单调递增，只增不减
// 2. 保证 memory threshold >= container memory limit
// 3. 仅在 micaexecutor 中记录 memoryThreshold
func (me *MicaExecutor) UpdateMemoryThreshold(ctx context.Context, memMiB uint32) error {
	me.mu.Lock()
	defer me.mu.Unlock()
	// 单调递增：如果当前阈值已经 >= 目标值，不需要更新
	if me.memoryThresholdMB >= memMiB {
		return nil
	}
	log.Debugf("update memory threshold: container=%s new=%d", me.ID, memMiB)
	if err := me.updateResource(ctx, MicaUpdateMemoryMax, resourceUint(memMiB)); err != nil {
		log.Warnf("failed to request new max memory \"%d\" to container: %v", memMiB, err)
		return err
	}
	// 单调递增：只更新更大的阈值
	me.memoryThresholdMB = max(memMiB, me.memoryThresholdMB)
	log.Debugf("update max memory threshold to %d", memMiB)
	return nil
}

// UpdateMemory updates the actual memory limit for the RTOS client.
// 映射关系：Container memory limit -> RTOS Client memory limit
// 同时保证 memory threshold >= container memory limit
func (me *MicaExecutor) UpdateMemory(ctx context.Context, memMiB uint32) error {
	me.mu.Lock()
	defer me.mu.Unlock()
	log.Debugf("update memory: container=%s old=%d new=%d", me.ID, me.records.memoryMB, memMiB)
	if err := me.updateResource(ctx, MicaUpdateMemoryCurrent, resourceUint(memMiB)); err != nil {
		log.Warnf("failed to request new memory \"%d\" to container: %v", memMiB, err)
		return err
	}
	// 更新 RTOS Client memory limit
	me.records.memoryMB = memMiB
	// 保证 memory threshold >= container memory limit
	me.memoryThresholdMB = max(memMiB, me.memoryThresholdMB)
	log.Debugf("update memory to %d", memMiB)
	return nil
}

func (me *MicaExecutor) ReadResource() *ports.ResourceSnapshot {
	me.mu.RLock()
	defer me.mu.RUnlock()

	res := &ports.ResourceSnapshot{}

	if me.records.vcpuNum > 0 {
		vcpu := me.records.vcpuNum
		res.VCPU = &vcpu
	}

	if me.records.cpuWeight > 0 {
		weight := me.records.cpuWeight
		res.CPUWeight = &weight
	}

	if me.records.cpuCapacity > 0 {
		capacity := me.records.cpuCapacity
		res.CPUCapacity = &capacity
	}

	if me.records.memoryMB > 0 {
		memory := me.records.memoryMB
		res.MemoryMaxMB = &memory
	}

	res.ClientCPUSet = strings.TrimRight(string(me.records.cpuStr[:]), "\x00")

	return res
}

func (me *MicaExecutor) VCPUPin(ctx context.Context, cpuList []int) error {
	cpustr := cpuset.NewCPUSet(cpuList...).String()
	if cpustr == "" {
		return fmt.Errorf("received cpuList %v, parsed into an empty array", cpuList)
	}

	return me.UpdatePCPUConstraints(ctx, cpustr)
}

func (me *MicaExecutor) NeedUpdateCPUCap(_ context.Context, target uint32) bool {
	me.mu.RLock()
	current := uint32(0)
	if me.records.cpuCapacity > 0 {
		current = me.records.cpuCapacity
	}
	me.mu.RUnlock()
	if me.Hypervisor == nil {
		return true
	}
	// No change: skip the hypervisor write. EXCEPT for target==0
	// ("unlimited"): records.cpuCapacity==0 is ambiguous (never recorded vs
	// unlimited — creation-time caps are not recorded, and a shim restart
	// loses them), so the hypervisor may still carry a stale cap. Always
	// write cap=0 so a lingering cap is actually cleared.
	if current == target && target != 0 {
		return false
	}
	return true
}

func (me *MicaExecutor) NeedUpdateMemLimit(target uint32) bool {
	return me.CurrentMaxMem() != target
}

// NeedUpdateMemThreshold checks if memory threshold needs to be updated.
// 内存资源映射规范：memoryThreshold 单调递增，只增不减
// 需要更新的条件：当前阈值 < 目标阈值
func (me *MicaExecutor) NeedUpdateMemThreshold(target uint32) bool {
	me.mu.RLock()
	defer me.mu.RUnlock()
	return me.memoryThresholdMB < target
}
func (me *MicaExecutor) NeedUpdateVCPUs(_ context.Context, target uint32) bool {
	if target == 0 {
		// 0 is a sentinel meaning "no vcpu change"; not an error.
		return false
	}
	// Over-host-max requests still need an update attempt: returning false
	// made updateVCPUCount treat the request as "already applied" and
	// silently succeed. UpdateVCPUNum rejects with a clear error.
	if me.Hypervisor == nil {
		return true
	}
	me.mu.RLock()
	current := uint32(0)
	if me.records.vcpuNum > 0 {
		current = me.records.vcpuNum
	}
	me.mu.RUnlock()
	return current != target
}

func (me *MicaExecutor) micaCtl(ctx context.Context, cmd MicaCommand, id string, opts ...string) error {
	return micaCtlWithHypervisor(ctx, me.Hypervisor, cmd, id, opts...)
}

func (me *MicaExecutor) NeedUpdateCPUSet(old, new string) bool {
	old = strings.TrimSpace(old)
	new = strings.TrimSpace(new)

	// Empty new means "cpuset not specified in this update" — never treat it
	// as a request to clear affinity. Callers that intentionally change the
	// set always pass a non-empty value (OCI linux.cpu.cpus).
	if new == "" {
		return false
	}

	// Fast-path: if caller provided old/new and they are identical, no update needed
	if old == new {
		return false
	}

	// Prefer comparing against our current recorded cpuset when available
	me.mu.RLock()
	current := strings.TrimRight(string(me.records.cpuStr[:]), "\x00")
	me.mu.RUnlock()
	current = strings.TrimSpace(current)
	if current != "" {
		return current != new
	}

	// If we don't know current state, conservatively update when new differs
	return old != new
}

func (me *MicaExecutor) NeedUpdateCPUWeight(target uint32) bool {
	me.mu.RLock()
	current := uint32(0)
	if me.records.cpuWeight > 0 {
		current = me.records.cpuWeight
	}
	me.mu.RUnlock()
	return current != target
}
