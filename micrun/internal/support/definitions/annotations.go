package defs

import ann "micrun/internal/support/annotations"

const (
	MicrunAnnotationPrefix = ann.MicrunAnnotationPrefix
	PedPrefix              = ann.PedPrefix
	RuntimePrefix          = ann.RuntimePrefix
	ContainerPrefix        = ann.ContainerPrefix
	CompatPrefix           = ann.CompatPrefix

	BundlePathKey        = ann.BundlePathKey
	ContainerTypeKey     = ann.ContainerTypeKey
	SandboxConfigPathKey = ann.SandboxConfigPathKey
)

const (
	OSAnnotation     = ann.OSAnnotation
	FirmwarePathAnno = ann.FirmwarePathAnno
	FirmwareHash     = ann.FirmwareHash

	AutoClose           = ann.AutoClose
	AutoCloseTimeout    = ann.AutoCloseTimeout
	OldAutoCloseTimeout = ann.OldAutoCloseTimeout

	Pedtype        = ann.Pedtype
	PedCompat      = ann.PedCompat
	NetPlaceholder = ann.NetPlaceholder
	PedestalConf   = ann.PedestalConf
)

const (
	ContainerMinMemMB   = ann.ContainerMinMemMB
	ContainerMaxVcpuNum = ann.ContainerMaxVcpuNum
)

const (
	DisableNewNetNs           = ann.DisableNewNetNs
	Experimental              = ann.Experimental
	PipeSize                  = ann.PipeSize
	RuntimeDebug              = ann.RuntimeDebug
	RuntimeMaxContainerCPUs   = ann.RuntimeMaxContainerCPUs
	RuntimeMaxContainerMemory = ann.RuntimeMaxContainerMemory
	RuntimePauseImage         = ann.RuntimePauseImage
	RuntimeExclusiveDom0CPU   = ann.RuntimeExclusiveDom0CPU
	RuntimeEnableVCPUsPinning = ann.RuntimeEnableVCPUsPinning
	RuntimeStaticResource     = ann.RuntimeStaticResource
	RuntimeHugePageEnable     = ann.RuntimeHugePageEnable
	VCPUBinding               = ann.VCPUBinding
)
