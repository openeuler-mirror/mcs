package shim

import (
	"testing"

	cntr "micrun/internal/domain/container"

	"github.com/stretchr/testify/require"
)

func TestWithMountedRootfsDoesNotMarkEmptyMountsAsMounted(t *testing.T) {
	rootfs := &cntr.RootFs{}
	called := false

	err := withMountedRootfs(t.TempDir(), nil, rootfs, func() error {
		called = true
		require.False(t, rootfs.Mounted)
		return nil
	})

	require.NoError(t, err)
	require.True(t, called)
	require.False(t, rootfs.Mounted)
}

func TestWithMountedRootfsRejectsNilState(t *testing.T) {
	err := withMountedRootfs(t.TempDir(), nil, nil, func() error {
		t.Fatal("run should not be called for nil rootfs state")
		return nil
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "rootfs state")
}

func TestMountRootfsReportsNoMountForEmptyMountList(t *testing.T) {
	mounted, err := mountRootfs(t.TempDir(), nil)

	require.NoError(t, err)
	require.False(t, mounted)
}
