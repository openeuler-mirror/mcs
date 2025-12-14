// Package types defines container types to replace kata-containers dependency.
package types

// ContainerType represents the type of a container.
// This is a local implementation to replace kata-containers dependency.
type ContainerType int

const (
	// PodContainer identifies a container that should be associated with an existing pod.
	PodContainer ContainerType = iota
	// PodSandbox identifies an infra container that will be used to create a pod.
	PodSandbox
	// SingleContainer is utilized to describe a container that doesn't have a container/sandbox
	// annotation applied. This is expected when dealing with non-pod containers (e.g., from ctr, podman).
	SingleContainer
	// SideCar identifies a sidecar container.
	SideCar
	// UnknownContainerType specifies a container that provides a container type annotation, but it is unknown.
	UnknownContainerType
)

// String returns the string representation of the container type.
func (ct ContainerType) String() string {
	switch ct {
	case PodContainer:
		return "PodContainer"
	case PodSandbox:
		return "PodSandbox"
	case SingleContainer:
		return "SingleContainer"
	case SideCar:
		return "SideCar"
	case UnknownContainerType:
		return "UnknownContainerType"
	default:
		return "UnknownContainerType"
	}
}