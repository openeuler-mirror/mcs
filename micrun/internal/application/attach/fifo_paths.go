package attach

import "micrun/internal/ports"

type attachFIFOPaths struct {
	stdin  string
	stdout string
	stderr string
}

func resolveAttachFIFOPaths(factory ports.IOSessionFactory, namespace, taskID string, terminal bool, attachInfo *ports.AttachInfo) attachFIFOPaths {
	paths := validAttachFIFOPaths(factory, attachInfo)
	if paths.needsDefaults(terminal, attachInfo) {
		paths.fillDefaults(factory, namespace, taskID, terminal, attachInfo)
	}
	return paths
}

func validAttachFIFOPaths(factory ports.IOSessionFactory, attachInfo *ports.AttachInfo) attachFIFOPaths {
	if attachInfo == nil {
		return attachFIFOPaths{}
	}

	paths := attachFIFOPaths{}
	if factory.IsValidFIFOPath(attachInfo.Stdin) {
		paths.stdin = attachInfo.Stdin
	}
	if factory.IsValidFIFOPath(attachInfo.Stdout) {
		paths.stdout = attachInfo.Stdout
	}
	if factory.IsValidFIFOPath(attachInfo.Stderr) {
		paths.stderr = attachInfo.Stderr
	}
	return paths
}

func (p attachFIFOPaths) needsDefaults(terminal bool, attachInfo *ports.AttachInfo) bool {
	if terminal {
		return p.stdin == "" || p.stdout == ""
	}
	if p.stdout == "" {
		return true
	}
	return attachInfo != nil && attachInfo.TTYErr != nil && p.stderr == ""
}

func (p *attachFIFOPaths) fillDefaults(factory ports.IOSessionFactory, namespace, taskID string, terminal bool, attachInfo *ports.AttachInfo) {
	if terminal {
		if p.stdin == "" {
			p.stdin = factory.GenerateFIFOPath(namespace, taskID, "stdin")
		}
		if p.stdout == "" {
			p.stdout = factory.GenerateFIFOPath(namespace, taskID, "stdout")
		}
		return
	}

	if p.stdout == "" {
		p.stdout = factory.GenerateFIFOPath(namespace, taskID, "stdout")
	}
	if attachInfo != nil && attachInfo.TTYErr != nil && p.stderr == "" {
		p.stderr = factory.GenerateFIFOPath(namespace, taskID, "stderr")
	}
}

func (p attachFIFOPaths) applyTo(attachInfo *ports.AttachInfo) {
	if attachInfo == nil {
		return
	}
	attachInfo.Stdin = p.stdin
	attachInfo.Stdout = p.stdout
	attachInfo.Stderr = p.stderr
}
