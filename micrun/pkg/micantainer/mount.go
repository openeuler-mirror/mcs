package micantainer

// Host filesystem mounting will be implemented well in future
type Mount struct {
	Type   string
	Source string
	Target string
	// the mounting destination inside the Client RTOS;
	MountDestInClient string
	// blkdev is the block device id attached to the Mica client
	// Currently, MICA is incapable of mounting block devices
	BlockDeviceID string
	Options       []string
	ReadOnly      bool
}

// RootFs represents the root filesystem of the container.
type RootFs struct {
	// Source is the path to the rootfs on the host.
	Source string
	Target string
	// Type is the filesystem type.
	Type string
	// Options are fstab-style mount options.
	Options []string
	// Mounted indicates whether the rootfs is mounted.

	Mounted bool
}
