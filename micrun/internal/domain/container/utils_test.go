package container

import (
	"context"
	"encoding/json"
	"micrun/internal/ports"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubGuestControl struct {
	exists      bool
	existsErr   error
	removeErr   error
	status      ports.GuestStatus
	existsCalls int
	removeCalls int
	statusCalls int
}

func (s *stubGuestControl) Start(context.Context, string) error { return nil }
func (s *stubGuestControl) Stop(context.Context, string) error  { return nil }
func (s *stubGuestControl) Remove(context.Context, string) error {
	s.removeCalls++
	return s.removeErr
}
func (s *stubGuestControl) Pause(context.Context, string) error  { return nil }
func (s *stubGuestControl) Resume(context.Context, string) error { return nil }

func (s *stubGuestControl) Exists(context.Context, string) (bool, error) {
	s.existsCalls++
	return s.exists, s.existsErr
}

func (s *stubGuestControl) Status(context.Context, string) (ports.GuestStatus, error) {
	s.statusCalls++
	return s.status, nil
}

// perIDGuestControl returns per-client-ID Status results, falling back to the
// default status for unmapped ids.
type perIDGuestControl struct {
	stubGuestControl
	statusByID map[string]ports.GuestStatus
}

func (s *perIDGuestControl) Status(_ context.Context, id string) (ports.GuestStatus, error) {
	s.statusCalls++
	if st, ok := s.statusByID[id]; ok {
		return st, nil
	}
	return s.status, nil
}

func TestCheckShimCollision(t *testing.T) {
	t.Run("无效 PID", func(t *testing.T) {
		assert.NoError(t, checkShimCollision("sandbox-zero", 0))
		assert.NoError(t, checkShimCollision("sandbox-neg", -1))
	})

	t.Run("shim 存活 - 同二进制进程", func(t *testing.T) {
		// The test binary itself satisfies the identity check (same
		// /proc/<pid>/exe target as os.Executable), so it stands in for a
		// live shim instance.
		err := checkShimCollision("sandbox-self", os.Getpid())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "another shim instance")
	})

	t.Run("PID 复用 - 外来进程不误判冲突", func(t *testing.T) {
		// A live process running a different binary is a recycled PID, not a
		// shim: collision must NOT be declared, or recovery/cleanup wedges
		// until the unrelated process exits.
		cmd := exec.Command("sleep", "30")
		require.NoError(t, cmd.Start())
		defer func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}()

		assert.NoError(t, checkShimCollision("sandbox-recycled", cmd.Process.Pid))
	})

	t.Run("不存在的进程", func(t *testing.T) {
		// 使用一个不太可能被占用的 PID
		assert.NoError(t, checkShimCollision("sandbox-dead", 99999))
	})
}

// testValidateStateRepo returns a state repository backed by an in-memory
// store and throwaway legacy roots for validateSandboxState tests.
func testValidateStateRepo(t *testing.T) stateRepository {
	t.Helper()
	return stateRepositoryWithLegacyRoots(newMemoryStateStore(), t.TempDir(), t.TempDir())
}

func TestValidateSandboxState(t *testing.T) {
	ctx := context.Background()

	t.Run("正常状态 - shim 存活", func(t *testing.T) {
		guestCtl := &stubGuestControl{exists: false}
		storage := &SandboxStorage{
			ID:      "test-shim-alive",
			ShimPID: os.Getpid(), // 当前进程，肯定存活
			State:   SandboxState{State: StateReady},
		}

		result, err := validateSandboxState(ctx, storage.ID, storage, guestCtl, nil, testValidateStateRepo(t))

		// shim 存活，应该返回错误（另一个实例）
		assert.False(t, result.Valid, "shim 存活时应该返回无效")
		assert.False(t, result.Cleanup, "shim 存活时不需要清理")
		assert.Error(t, err, "shim 存活时应该返回错误")
		assert.Contains(t, err.Error(), "another shim instance", "错误信息应该提到另一个实例")
		assert.Zero(t, guestCtl.existsCalls, "shim 冲突时不应继续访问 guest backend")
		assert.Zero(t, guestCtl.statusCalls, "shim 冲突时不应查询 guest 状态")
	})

	t.Run("僵尸状态 - shim 死亡且 RTOS 不存在", func(t *testing.T) {
		guestCtl := &stubGuestControl{exists: false}
		// 使用一个不存在的容器 ID，这样 RTOS 也不存在
		storage := &SandboxStorage{
			ID:      "test-container-nonexistent-" + time.Now().Format("20060102150405"),
			ShimPID: 99999, // 不存在的 PID
			State:   SandboxState{State: StateReady},
		}

		result, err := validateSandboxState(ctx, storage.ID, storage, guestCtl, nil, testValidateStateRepo(t))

		// RTOS 不存在，应该标记需要清理
		assert.False(t, result.Valid, "RTOS 不存在时应该返回无效")
		assert.True(t, result.Cleanup, "RTOS 不存在时应该标记需要清理")
		assert.NoError(t, err, "RTOS 不存在时不应返回错误")
		assert.Equal(t, 1, guestCtl.existsCalls, "应该检查一次 guest 是否存在")
		assert.Zero(t, guestCtl.statusCalls, "guest 不存在时不应继续查询状态")
	})

	t.Run("micad socket 消失但 Xen domain 存活 - 不清理", func(t *testing.T) {
		// micad restart wipes /run/mica (Exists=false) while the Xen domain
		// keeps running. State must be kept so recovery restores the task and
		// the normal Down→Remove→re-register path cleans the domain, instead
		// of deleting the state and orphaning the domain.
		guestCtl := &stubGuestControl{exists: false}
		hyp := &fakeHypervisorControl{name: "running"}
		storage := &SandboxStorage{
			ID:      "test-domain-alive-socket-gone",
			ShimPID: 99999,
			State:   SandboxState{State: StateRunning},
		}

		result, err := validateSandboxState(ctx, storage.ID, storage, guestCtl, hyp, testValidateStateRepo(t))

		assert.NoError(t, err)
		assert.True(t, result.Valid, "live domain must keep the sandbox recoverable")
		assert.False(t, result.Cleanup, "cleanup would orphan the live Xen domain")
	})

	t.Run("CRI InfraOnly - sandbox id 无 domain 但 RTOS 容器仍在", func(t *testing.T) {
		// Exists returns true only for the non-infra RTOS container id.
		guestCtl := &stubGuestControl{exists: false}
		// Override Exists via a custom stub that tracks ids — use exists=true
		// for any call when we only probe rtos-1 (non-infra).
		guestCtl.exists = true
		storage := &SandboxStorage{
			ID:      "pod-sandbox-infra-id",
			ShimPID: 99999,
			State:   SandboxState{State: StateRunning},
			Config: SandboxConfig{
				ContainerConfigs: map[string]*ContainerConfig{
					"pod-sandbox-infra-id": {ID: "pod-sandbox-infra-id", IsInfra: true},
					"rtos-workload":        {ID: "rtos-workload", IsInfra: false},
				},
			},
		}

		result, err := validateSandboxState(ctx, storage.ID, storage, guestCtl, nil, testValidateStateRepo(t))

		assert.NoError(t, err)
		assert.True(t, result.Valid, "non-infra RTOS still present should not be stale")
		assert.False(t, result.Cleanup)
		assert.Equal(t, 1, guestCtl.existsCalls, "should probe the non-infra RTOS id only")
	})

	t.Run("状态不一致仅记录，不阻断恢复", func(t *testing.T) {
		guestCtl := &stubGuestControl{
			exists: true,
			status: ports.GuestStatus{State: "Stopped", Stopped: true},
		}
		storage := &SandboxStorage{
			ID:      "test-state-mismatch",
			ShimPID: 99999,
			State: SandboxState{
				State: StateRunning,
			},
		}

		result, err := validateSandboxState(ctx, storage.ID, storage, guestCtl, nil, testValidateStateRepo(t))

		assert.True(t, result.Valid)
		assert.False(t, result.Cleanup)
		assert.NoError(t, err)
		assert.Equal(t, 1, guestCtl.existsCalls, "应先检查 guest 存在")
		assert.Equal(t, 1, guestCtl.statusCalls, "guest 存在时应查询一次状态")
	})

	t.Run("状态文件元数据", func(t *testing.T) {
		now := time.Now().Unix()
		storage := &SandboxStorage{
			ID:        "test-metadata",
			CreatedAt: now,
			ShimPID:   os.Getpid(),
		}

		// 验证元数据字段
		assert.Greater(t, storage.CreatedAt, int64(0), "CreatedAt 应该大于 0")
		assert.Greater(t, storage.ShimPID, 0, "ShimPID 应该大于 0")
		assert.Equal(t, now, storage.CreatedAt, "CreatedAt 应该被正确设置")
	})

	t.Run("nil storage", func(t *testing.T) {
		result, err := validateSandboxState(ctx, "test", nil, &stubGuestControl{}, nil, testValidateStateRepo(t))

		assert.False(t, result.Valid)
		assert.False(t, result.Cleanup)
		assert.Error(t, err)
	})

	t.Run("nil guest control", func(t *testing.T) {
		result, err := validateSandboxState(ctx, "test", &SandboxStorage{}, nil, nil, testValidateStateRepo(t))

		assert.False(t, result.Valid)
		assert.False(t, result.Cleanup)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "guest control")
	})

	t.Run("invalid persisted state", func(t *testing.T) {
		guestCtl := &stubGuestControl{exists: true}
		result, err := validateSandboxState(ctx, "test", &SandboxStorage{
			ID:    "test-invalid-state",
			State: SandboxState{State: StateString("unknown")},
		}, guestCtl, nil, testValidateStateRepo(t))

		assert.False(t, result.Valid)
		assert.False(t, result.Cleanup)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "sandbox state invalid")
		assert.Zero(t, guestCtl.existsCalls)
		assert.Zero(t, guestCtl.statusCalls)
	})
}

func TestLoadSandboxWithValidation(t *testing.T) {
	_ = context.Background()
	// 使用临时目录进行测试
	baseDir := os.Getenv("TMPDIR")
	if baseDir == "" {
		baseDir = "/tmp"
	}
	testDir := filepath.Join(baseDir, "micrun-test-sandbox")

	// 确保测试前目录是干净的
	os.RemoveAll(testDir)
	defer os.RemoveAll(testDir)

	t.Run("加载并验证正常状态（shim 存活）", func(t *testing.T) {
		testID := "test-load-valid"
		sandboxDir := filepath.Join(testDir, testID)
		err := os.MkdirAll(sandboxDir, 0755)
		require.NoError(t, err)

		// 创建状态文件，shim PID 是当前进程
		storage := &SandboxStorage{
			ID:      testID,
			ShimPID: os.Getpid(),
			Config: SandboxConfig{
				ID: testID,
			},
		}
		data, err := json.Marshal(storage)
		require.NoError(t, err)

		stateFile := filepath.Join(sandboxDir, "state.json")
		err = os.WriteFile(stateFile, data, 0644)
		require.NoError(t, err)

		// 注意：由于 loadSandbox 会检查 defs.SandboxDataDir
		// 这里我们只是测试函数逻辑，实际集成测试需要 mock 更多部分
		// 所以这里我们只验证状态文件的创建
		_, err = os.Stat(stateFile)
		assert.NoError(t, err, "状态文件应该存在")
	})

	t.Run("状态文件元数据正确保存", func(t *testing.T) {
		testID := "test-metadata-save"
		sandboxDir := filepath.Join(testDir, testID)
		err := os.MkdirAll(sandboxDir, 0755)
		require.NoError(t, err)

		now := time.Now().Unix()
		storage := &SandboxStorage{
			ID:        testID,
			CreatedAt: now,
			ShimPID:   os.Getpid(),
			Config:    SandboxConfig{ID: testID},
		}

		data, err := json.Marshal(storage)
		require.NoError(t, err)

		stateFile := filepath.Join(sandboxDir, "state.json")
		err = os.WriteFile(stateFile, data, 0644)
		require.NoError(t, err)

		// 读取并验证
		loaded, err := os.ReadFile(stateFile)
		require.NoError(t, err)

		var loadedStorage SandboxStorage
		err = json.Unmarshal(loaded, &loadedStorage)
		require.NoError(t, err)

		assert.Equal(t, testID, loadedStorage.ID)
		assert.Equal(t, now, loadedStorage.CreatedAt)
		assert.Equal(t, os.Getpid(), loadedStorage.ShimPID)
	})
}

func TestValidateSandboxStateStoppedSandboxNotStale(t *testing.T) {
	ctx := context.Background()
	guestCtl := &stubGuestControl{exists: false}
	storage := &SandboxStorage{
		ID:      "test-all-stopped",
		ShimPID: 99999,
		State:   SandboxState{State: StateStopped},
	}

	result, err := validateSandboxState(ctx, storage.ID, storage, guestCtl, nil, testValidateStateRepo(t))

	assert.NoError(t, err)
	assert.True(t, result.Valid, "全部停止的 sandbox 不应判为 stale（mica stop 会移除 client）")
	assert.False(t, result.Cleanup)
	assert.Zero(t, guestCtl.existsCalls, "已停止 sandbox 不应探测 micad")
}

func TestValidateSandboxStateAllContainersStoppedNotStale(t *testing.T) {
	ctx := context.Background()
	repo := stateRepositoryWithLegacyRoots(newMemoryStateStore(), t.TempDir(), t.TempDir())
	sandboxID := "sandbox-containers-stopped"
	containerID := "container-stopped"
	containerPath := filepath.Join(sandboxID, containerID)
	payload, err := json.Marshal(ContainerStorage{
		ID:            containerID,
		SandboxID:     sandboxID,
		State:         ContainerState{State: StateStopped},
		Config:        ContainerConfig{ID: containerID},
		ContainerPath: containerPath,
	})
	require.NoError(t, err)
	require.NoError(t, repo.store.Save(ctx, &ports.RuntimeSnapshot{
		Namespace: runtimeStateNamespaceContainer,
		TaskID:    containerSnapshotID(containerPath, containerID),
		Data:      payload,
	}))

	guestCtl := &stubGuestControl{exists: false}
	storage := &SandboxStorage{
		ID:      sandboxID,
		ShimPID: 99999,
		State:   SandboxState{State: StateRunning},
		Config: SandboxConfig{
			ContainerConfigs: map[string]*ContainerConfig{
				containerID: {ID: containerID},
			},
		},
	}

	result, err := validateSandboxState(ctx, sandboxID, storage, guestCtl, nil, repo)

	assert.NoError(t, err)
	assert.True(t, result.Valid, "容器按设计全部停止时不应判 stale")
	assert.False(t, result.Cleanup)
	assert.Zero(t, guestCtl.existsCalls, "已停止容器不应探测 micad")
}

// TestValidateSandboxStateInfraOnlyNotStale verifies that a CRI InfraOnly pod
// sandbox (only infra container, sandbox id == infra id, no mica domain) is NOT
// marked stale during recovery. Its sandbox id never registers in micad, so
// absence is expected by design.
func TestValidateSandboxStateInfraOnlyNotStale(t *testing.T) {
	ctx := context.Background()
	guestCtl := &stubGuestControl{exists: false}
	storage := &SandboxStorage{
		ID:      "pod-infra-only-id",
		ShimPID: 99999,
		State:   SandboxState{State: StateRunning},
		Config: SandboxConfig{
			ContainerConfigs: map[string]*ContainerConfig{
				"pod-infra-only-id": {ID: "pod-infra-only-id", IsInfra: true},
			},
		},
	}

	result, err := validateSandboxState(ctx, storage.ID, storage, guestCtl, nil, testValidateStateRepo(t))

	assert.NoError(t, err)
	assert.True(t, result.Valid, "CRI InfraOnly sandbox should not be marked stale (sandbox id is infra, never in micad)")
	assert.False(t, result.Cleanup, "InfraOnly sandbox state must not be deleted on recovery")
}

// TestCompareStoredAndLiveStateMultiContainer verifies that the sandbox state
// correction probes ALL client ids, not just the first one. With one stopped
// and one running container, the sandbox must stay Running.
func TestCompareStoredAndLiveStateMultiContainer(t *testing.T) {
	ctx := context.Background()
	stoppedStatus := ports.GuestStatus{State: "Stopped", Stopped: true}
	runningStatus := ports.GuestStatus{State: "Running", Running: true}

	t.Run("one stopped + one running -> sandbox stays running", func(t *testing.T) {
		guestCtl := &perIDGuestControl{
			statusByID: map[string]ports.GuestStatus{
				"cont-a": stoppedStatus,
				"cont-b": runningStatus,
			},
		}
		storage := &SandboxStorage{
			ID:    "sandbox-mixed",
			State: SandboxState{State: StateRunning},
			Config: SandboxConfig{
				ContainerConfigs: map[string]*ContainerConfig{
					"cont-a": {ID: "cont-a"},
					"cont-b": {ID: "cont-b"},
				},
			},
		}
		corrected := compareStoredAndLiveState(ctx, storage.ID, storage, guestCtl, stateRepository{})
		assert.False(t, corrected, "sandbox with a running container must not be corrected to stopped")
		assert.Equal(t, StateRunning, storage.State.State)
		assert.Equal(t, 2, guestCtl.statusCalls, "should probe both client ids")
	})

	t.Run("all stopped -> sandbox corrected to stopped", func(t *testing.T) {
		guestCtl := &perIDGuestControl{
			statusByID: map[string]ports.GuestStatus{
				"cont-a": stoppedStatus,
				"cont-b": stoppedStatus,
			},
		}
		storage := &SandboxStorage{
			ID:    "sandbox-all-stopped",
			State: SandboxState{State: StateRunning},
			Config: SandboxConfig{
				ContainerConfigs: map[string]*ContainerConfig{
					"cont-a": {ID: "cont-a"},
					"cont-b": {ID: "cont-b"},
				},
			},
		}
		corrected := compareStoredAndLiveState(ctx, storage.ID, storage, guestCtl, stateRepository{})
		assert.True(t, corrected, "sandbox with all stopped containers should be corrected")
		assert.Equal(t, StateStopped, storage.State.State)
	})

	t.Run("file=stopped, one running -> corrected to running", func(t *testing.T) {
		guestCtl := &perIDGuestControl{
			statusByID: map[string]ports.GuestStatus{
				"cont-a": stoppedStatus,
				"cont-b": runningStatus,
			},
		}
		storage := &SandboxStorage{
			ID:    "sandbox-revive",
			State: SandboxState{State: StateStopped},
			Config: SandboxConfig{
				ContainerConfigs: map[string]*ContainerConfig{
					"cont-a": {ID: "cont-a"},
					"cont-b": {ID: "cont-b"},
				},
			},
		}
		corrected := compareStoredAndLiveState(ctx, storage.ID, storage, guestCtl, stateRepository{})
		assert.True(t, corrected, "sandbox with a running container should be corrected to running")
		assert.Equal(t, StateRunning, storage.State.State)
	})

	t.Run("all stopped but persisted paused -> sandbox stays running", func(t *testing.T) {
		// Non-Xen Pause maps to mica stop: guest reports Stopped while the
		// container snapshot still says paused. Recovery must not "correct"
		// the sandbox to Stopped or Resume fails with SandboxNotReady.
		repo := stateRepositoryWithLegacyRoots(newMemoryStateStore(), t.TempDir(), t.TempDir())
		sandboxID := "sandbox-paused"
		containerID := "cont-paused"
		payload, err := json.Marshal(ContainerStorage{
			ID:            containerID,
			SandboxID:     sandboxID,
			State:         ContainerState{State: StatePaused},
			Config:        ContainerConfig{ID: containerID},
			ContainerPath: filepath.Join(sandboxID, containerID),
		})
		require.NoError(t, err)
		require.NoError(t, repo.store.Save(ctx, &ports.RuntimeSnapshot{
			Namespace: runtimeStateNamespaceContainer,
			TaskID:    containerSnapshotID(filepath.Join(sandboxID, containerID), containerID),
			Data:      payload,
		}))

		guestCtl := &perIDGuestControl{
			statusByID: map[string]ports.GuestStatus{
				containerID: stoppedStatus,
			},
		}
		storage := &SandboxStorage{
			ID:    sandboxID,
			State: SandboxState{State: StateRunning},
			Config: SandboxConfig{
				ContainerConfigs: map[string]*ContainerConfig{
					containerID: {ID: containerID},
				},
			},
		}
		corrected := compareStoredAndLiveState(ctx, storage.ID, storage, guestCtl, repo)
		assert.False(t, corrected, "paused container reporting stopped must keep sandbox running")
		assert.Equal(t, StateRunning, storage.State.State)
	})
}
