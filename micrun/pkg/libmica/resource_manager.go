package libmica

import (
	"fmt"
	log "micrun/logger"
	"micrun/pkg/pedestal"
	"strconv"
	"strings"
)

const cpuCapRatio = 100

// TODO: this function is not in use now, we migrate from `UpdateVCPUs` to this function
// after allocating sandbox cpus in xen pool is finished.
func (me *MicaExecutor) UpdateSandboxPoolVCPUs() {}

// MemoryThresholdMB returns the current memory threshold in MiB.
// 内存资源映射规范：
// 1. Container memory limit -> RTOS Client memory limit
// 2. Container memory reservation -> RTOS Client memory min
// 3. memoryThreshold 仅在 micaexecutor 中记录，保证 memory threshold >= container memory limit
func (me *MicaExecutor) MemoryThresholdMB() uint32 {
	return me.memoryThresholdMB
}

// CurrentMaxMem returns the current memory limit in MiB.
// 对应 RTOS Client memory limit，来自 Container memory limit
func (me *MicaExecutor) CurrentMaxMem() uint32 {
	if me.records.memoryMB <= 0 {
		return 0
	}
	return uint32(me.records.memoryMB)
}

// RecordMemoryState records the current memory state.
// memoryThreshold 设计为单调递增的，仅在新的 memory threshold 出现时才会正向更新
func (me *MicaExecutor) RecordMemoryState(current, threshold uint32) {
	me.records.memoryMB = int(current)
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
func (me *MicaExecutor) EnsureMemoryLimit(target uint32) error {
	current := me.CurrentMaxMem()
	threshold := me.MemoryThresholdMB()

	if threshold == 0 {
		threshold = current
	}

	// 保证 memory threshold >= container memory limit
	if threshold < target {
		if err := me.UpdateMemoryThreshold(target); err != nil {
			return err
		}
		// 单调递增：只更新更大的阈值
		me.memoryThresholdMB = target
	}

	if current == target {
		return nil
	}

	if err := me.UpdateMemory(target); err != nil {
		return err
	}

	return nil
}

// number of visible vcpus
func (me *MicaExecutor) UpdateVCPUNum(newVCPUs uint32) (oldCPUs, newCPUs uint32, retErr error) {
	log.Debugf("update vcpu num: container=%s old=%d new=%d", me.Id, me.records.vcpuNum, newVCPUs)
	cmdArgs := []string{"VCPU", strconv.Itoa(int(newVCPUs))}
	s := strings.Join(cmdArgs, " ")
	err := micaCtl(MUpdate, me.Id, s)
	if err != nil {
		log.Warnf("failed to update vcpu number: %v", err)
		return uint32(me.records.vcpuNum), uint32(me.records.vcpuNum), err
	}
	return uint32(me.records.vcpuNum), newVCPUs, err
}

// TODO: temporarily dirty-join string as command line, need to change to a better way
func (me *MicaExecutor) UpdatePCPUConstrains(cpus string) error {
	log.Debugf("update pcpu constraints: container=%s cpuset=%s", me.Id, cpus)
	cmdArgs := []string{"CPU", cpus}
	s := strings.Join(cmdArgs, " ")
	err := micaCtl(MUpdate, me.Id, s)
	if err != nil {
		log.Warnf("failed to bind physical cpuset \"%s\" to container: %v", cpus, err)
	} else {
		me.records.cpuStr = [MaxCPUStringLen]byte{}
		copy(me.records.cpuStr[:], []byte(cpus))
		log.Debugf("updated to new cpuset: %s", cpus)
	}
	return err
}

func (me *MicaExecutor) UpdateCPUCapacity(cap uint32) error {
	log.Debugf("update cpu capacity: container=%s old=%d new=%d", me.Id, me.records.cpuCapacity, cap)
	cmdArgs := []string{"CPUCpacity", strconv.Itoa(int(cap))}
	s := strings.Join(cmdArgs, " ")
	err := micaCtl(MUpdate, me.Id, s)
	if err != nil {
		log.Warnf("failed to update cap time to %d that container can run: %v", cap, err)
	} else {
		me.records.cpuCapacity = int(cap)
		log.Debugf("updated to new cpu capacity: %d", cap)
	}
	return err
}

func (me *MicaExecutor) UpdateCPUWeight(weight uint32) error {
	log.Debugf("update cpu weight: container=%s old=%d new=%d", me.Id, me.records.cpuWeight, weight)
	cmdArgs := []string{"CPUWeight", strconv.Itoa(int(weight))}
	s := strings.Join(cmdArgs, " ")
	err := micaCtl(MUpdate, me.Id, s)
	if err != nil {
		log.Warnf("failed to update cpu share time to %d that container can run: %v", weight, err)
	} else {
		me.records.cpuWeight = int(weight)
		log.Debugf("updated to new cpu weight: %d", weight)
	}
	return err
}

// UpdateMemoryThreshold updates the memory threshold for the RTOS client.
// NOTICE: MemoryThreshold is not the max memory of client. It is the max memory
// that pedestal can allocate to container (pedestal max memory).
// Memory is just the max memory of a client.
// 内存资源映射规范：
// 1. memoryThreshold 单调递增，只增不减
// 2. 保证 memory threshold >= container memory limit
// 3. 仅在 micaexecutor 中记录 memoryThreshold
func (me *MicaExecutor) UpdateMemoryThreshold(memMiB uint32) error {
	// 单调递增：如果当前阈值已经 >= 目标值，不需要更新
	if me.memoryThresholdMB >= memMiB {
		return nil
	}
	log.Debugf("update memory threshold: container=%s old=%d new=%d", me.Id, me.records.memoryMB, memMiB)
	cmdArgs := []string{"MaxMem", strconv.Itoa(int(memMiB))}
	s := strings.Join(cmdArgs, " ")
	err := micaCtl(MUpdate, me.Id, s)
	if err != nil {
		log.Warnf("failed to request new max memory \"%d\" to container: %v", memMiB, err)
	} else {
		// 单调递增：只更新更大的阈值
		me.memoryThresholdMB = max(memMiB, me.memoryThresholdMB)
		log.Debugf("update max memory threshold to %d", memMiB)
	}
	return err
}

// UpdateMemory updates the actual memory limit for the RTOS client.
// 映射关系：Container memory limit -> RTOS Client memory limit
// 同时保证 memory threshold >= container memory limit
func (me *MicaExecutor) UpdateMemory(memMiB uint32) error {
	log.Debugf("update memory: container=%s old=%d new=%d", me.Id, me.records.memoryMB, memMiB)
	cmdArgs := []string{"Memory", strconv.Itoa(int(memMiB))}
	s := strings.Join(cmdArgs, " ")
	err := micaCtl(MUpdate, me.Id, s)
	if err != nil {
		log.Warnf("failed to request new memory \"%d\" to container: %v", memMiB, err)
	} else {
		// 更新 RTOS Client memory limit
		me.records.memoryMB = int(memMiB)
		// 保证 memory threshold >= container memory limit
		me.memoryThresholdMB = max(memMiB, me.memoryThresholdMB)
		log.Debugf("update memory to %d", memMiB)
	}
	return err
}

func (me *MicaExecutor) ReadResource() *pedestal.EssentialResource {
	res := pedestal.InitResource()

	// Initialize pointer fields with values from records
	if me.records.vcpuNum > 0 {
		vcpu := uint32(me.records.vcpuNum)
		res.Vcpu = &vcpu
	}

	if me.records.cpuWeight > 0 {
		weight := uint32(me.records.cpuWeight)
		res.CPUWeight = &weight
	} else {
		res.CPUWeight = nil
	}

	if me.records.cpuCapacity > 0 {
		capacity := uint32(me.records.cpuCapacity)
		res.CpuCpacity = &capacity
	}

	if me.records.memoryMB > 0 {
		memory := uint32(me.records.memoryMB)
		res.MemoryMaxMB = &memory
	} else {
		res.MemoryMaxMB = nil
	}

	// Set ClientCpuSet from cpuStr (convert byte array to string)
	res.ClientCpuSet = strings.TrimRight(string(me.records.cpuStr[:]), "\x00")

	return res
}

func (me *MicaExecutor) VcpuPin(cpuList []int) error {
	cpustr := pedestal.ParseCPUArr(cpuList)
	if cpustr == "" {
		return fmt.Errorf("received cpuList %v, parsed into an empty array", cpuList)
	}

	return me.UpdatePCPUConstrains(cpustr)
}

func (me *MicaExecutor) NeedUpdateCpuCap(target uint32) bool {
	current := uint32(0)
	if me.records.cpuCapacity > 0 {
		current = uint32(me.records.cpuCapacity)
	}
	hostCPUs := pedestal.HostCPUCounts().Physical
	if current == target && target >= uint32(cpuCapRatio)*hostCPUs {
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
	return me.memoryThresholdMB < target
}
func (me *MicaExecutor) NeedUpdateVCpus(target uint32) bool {
	maxCPUs := pedestal.HostCPUCounts().Physical
	if target == 0 || target > maxCPUs {
		return false
	}
	current := uint32(0)
	if me.records.vcpuNum > 0 {
		current = uint32(me.records.vcpuNum)
	}
	return current != target
}

func (me *MicaExecutor) NeedUpdateCpuSet(old, new string) bool {
	old = strings.TrimSpace(old)
	new = strings.TrimSpace(new)

	// Fast-path: if caller provided old/new and they are identical, no update needed
	if old == new {
		return false
	}

	// Prefer comparing against our current recorded cpuset when available
	current := strings.TrimRight(string(me.records.cpuStr[:]), "\x00")
	current = strings.TrimSpace(current)
	if current != "" {
		return current != new
	}

	// If we don't know current state, conservatively update when new is non-empty or differs
	return old != new
}

func (me *MicaExecutor) NeedUpdateCpuShare(target uint32) bool {
	current := uint32(0)
	if me.records.cpuWeight > 0 {
		current = uint32(me.records.cpuWeight)
	}
	return current != target
}
