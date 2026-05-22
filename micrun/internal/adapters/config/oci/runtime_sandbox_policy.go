package oci

type RuntimeSandboxPolicy struct {
	MiniVCPUNum      uint32
	ExclusiveDom0CPU bool
}

func defaultRuntimeSandboxPolicy() RuntimeSandboxPolicy {
	return RuntimeSandboxPolicy{}
}

func (r *RuntimeConfig) SetExclusiveDom0CPU(flag string) {
	enabled, ok := parseRuntimeBool(KeyExclusiveDom0CPU, flag)
	if !ok {
		return
	}
	r.ExclusiveDom0CPU = enabled
}

func (r *RuntimeConfig) SetMiniVCPUNum(miniVCPUString string) {
	miniVCPU, ok := parseRuntimeUint32(KeySandboxMinVCPU, miniVCPUString)
	if !ok {
		return
	}
	r.MiniVCPUNum = miniVCPU
}
