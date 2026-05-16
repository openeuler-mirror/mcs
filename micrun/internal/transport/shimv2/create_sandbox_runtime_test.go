package shim

import (
	"strings"
	"testing"

	cntr "micrun/internal/domain/container"

	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
)

func TestPropagateNetworkNamespaceAnnotationInitializesMaps(t *testing.T) {
	ociSpec := &specs.Spec{}
	sandboxConfig := &cntr.SandboxConfig{
		NetworkConfig: cntr.NetworkConfig{NetworkID: "/proc/123/ns/net"},
	}

	propagateNetworkNamespaceAnnotation(ociSpec, sandboxConfig)

	require.Equal(t, "/proc/123/ns/net", ociSpec.Annotations[nerdctlNetworkNamespaceAnnotation])
	require.Equal(t, "/proc/123/ns/net", sandboxConfig.Annotations[nerdctlNetworkNamespaceAnnotation])
}

func TestPropagateNetworkNamespaceAnnotationPreservesOtherAnnotations(t *testing.T) {
	ociSpec := &specs.Spec{Annotations: map[string]string{"oci": "keep"}}
	sandboxConfig := &cntr.SandboxConfig{
		Annotations:   map[string]string{"sandbox": "keep"},
		NetworkConfig: cntr.NetworkConfig{NetworkID: "/proc/456/ns/net"},
	}

	propagateNetworkNamespaceAnnotation(ociSpec, sandboxConfig)

	require.Equal(t, "keep", ociSpec.Annotations["oci"])
	require.Equal(t, "keep", sandboxConfig.Annotations["sandbox"])
	require.Equal(t, "/proc/456/ns/net", ociSpec.Annotations[nerdctlNetworkNamespaceAnnotation])
	require.Equal(t, "/proc/456/ns/net", sandboxConfig.Annotations[nerdctlNetworkNamespaceAnnotation])
}

func TestPropagateNetworkNamespaceAnnotationAllowsNilInputs(t *testing.T) {
	require.NotPanics(t, func() {
		propagateNetworkNamespaceAnnotation(nil, nil)
	})
}

func TestInjectUnmountedRootfsResolvesAndStoresSingleContainerRootfs(t *testing.T) {
	rootfsSource := t.TempDir()
	containerConfig := &cntr.ContainerConfig{}
	sandboxConfig := &cntr.SandboxConfig{
		ContainerConfigs: map[string]*cntr.ContainerConfig{
			"container-1": containerConfig,
		},
	}

	err := injectUnmountedRootfs("container-1", cntr.RootFs{
		Source:  rootfsSource,
		Type:    "bind",
		Options: []string{"ro"},
	}, sandboxConfig)

	require.NoError(t, err)
	require.Equal(t, rootfsSource, containerConfig.Rootfs.Source)
	require.Equal(t, "bind", containerConfig.Rootfs.Type)
	require.Equal(t, []string{"ro"}, containerConfig.Rootfs.Options)
}

func TestInjectUnmountedRootfsSkipsMountedOrMultiContainerConfigs(t *testing.T) {
	containerConfig := &cntr.ContainerConfig{}
	sandboxConfig := &cntr.SandboxConfig{
		ContainerConfigs: map[string]*cntr.ContainerConfig{
			"container-1": containerConfig,
			"container-2": {},
		},
	}

	require.NoError(t, injectUnmountedRootfs("container-1", cntr.RootFs{Source: "unused"}, sandboxConfig))
	require.Empty(t, containerConfig.Rootfs.Source)

	sandboxConfig.ContainerConfigs = map[string]*cntr.ContainerConfig{"container-1": containerConfig}
	require.NoError(t, injectUnmountedRootfs("container-1", cntr.RootFs{Mounted: true, Source: "unused"}, sandboxConfig))
	require.Empty(t, containerConfig.Rootfs.Source)
}

func TestInjectUnmountedRootfsReturnsMissingContainerConfigError(t *testing.T) {
	sandboxConfig := &cntr.SandboxConfig{
		ContainerConfigs: map[string]*cntr.ContainerConfig{
			"other": {},
		},
	}

	err := injectUnmountedRootfs("container-1", cntr.RootFs{}, sandboxConfig)

	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "container-1"))
}
