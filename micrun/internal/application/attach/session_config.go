package attach

import "micrun/internal/ports"

func ioSessionConfigFromAttachInfo(containerID string, attachInfo ports.AttachInfo) ports.IOSessionConfig {
	return ports.IOSessionConfig{
		ContainerID: containerID,
		StdinFIFO:   attachInfo.Stdin,
		StdoutFIFO:  attachInfo.Stdout,
		StderrFIFO:  attachInfo.Stderr,
		TTYIn:       attachInfo.TTYIn,
		TTYOut:      attachInfo.TTYOut,
		TTYErr:      attachInfo.TTYErr,
		Terminal:    attachInfo.Terminal,
		FilterNUL:   true,
	}
}
