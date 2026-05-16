package container

import (
	defs "micrun/internal/support/definitions"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func (cfg *ContainerConfig) ensureMemory() *specs.LinuxMemory {
	res := cfg.ensureResources()
	if res == nil {
		return nil
	}
	if res.Memory == nil {
		res.Memory = &specs.LinuxMemory{}
	}
	return res.Memory
}

func (cfg *ContainerConfig) containerMaxMemMB() uint32 {
	lim := cfg.memoryLimitMB()
	if lim != 0 {
		return lim
	}
	lim = cfg.memoryReservationMB()
	if lim != 0 {
		return lim
	}
	return defs.DefaultMinMemMB
}

func (cfg *ContainerConfig) memoryLimitMB() uint32 {
	return bytesToMiB(cfg.memoryLimitBytes())
}

func (cfg *ContainerConfig) memoryReservationMB() uint32 {
	return bytesToMiB(cfg.memoryReservationBytes())
}

func (cfg *ContainerConfig) memoryLimitBytes() *int64 {
	mem := cfg.memorySpec()
	if mem == nil {
		return nil
	}
	return mem.Limit
}

func (cfg *ContainerConfig) memoryReservationBytes() *int64 {
	mem := cfg.memorySpec()
	if mem == nil {
		return nil
	}
	return mem.Reservation
}

func (cfg *ContainerConfig) setMemoryReservationMB(mb uint32) {
	mem := cfg.ensureMemory()
	if mem == nil {
		return
	}
	mem.Reservation = miBToBytes(mb)
}

// MemoryLimitMiB returns the configured memory limit in MiB.
// 映射关系：Container memory limit -> RTOS Client memory limit
// memoryThreshold 仅在 micaexecutor 中记录，保证 memory threshold >= container memory limit
func (cfg *ContainerConfig) MemoryLimitMiB() uint32 {
	return cfg.memoryLimitMB()
}

// MemoryReservationMiB returns the configured memory reservation in MiB.
// 映射关系：Container memory reservation -> RTOS Client memory min
// 保证：memory reservation < memory limit
func (cfg *ContainerConfig) MemoryReservationMiB() uint32 {
	return cfg.memoryReservationMB()
}

// SetMemoryReservationMB records the requested memory reservation.
// 用于设置 RTOS Client memory min，必须小于 memory limit
func (cfg *ContainerConfig) SetMemoryReservationMB(mb uint32) {
	cfg.setMemoryReservationMB(mb)
}
