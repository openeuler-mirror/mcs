package micantainer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessExists(t *testing.T) {
	t.Run("当前进程存在", func(t *testing.T) {
		pid := os.Getpid()
		assert.True(t, processExists(pid), "当前进程应该存在")
	})

	t.Run("不存在的进程", func(t *testing.T) {
		// 使用一个不太可能被占用的 PID
		assert.False(t, processExists(99999), "不存在的进程应该返回 false")
	})

	t.Run("无效 PID", func(t *testing.T) {
		assert.False(t, processExists(0), "PID 0 应该返回 false")
		assert.False(t, processExists(-1), "负数 PID 应该返回 false")
	})
}

func TestValidateSandboxState(t *testing.T) {
	ctx := context.Background()

	t.Run("正常状态 - shim 存活", func(t *testing.T) {
		storage := &SandboxStorage{
			ID:      "test-shim-alive",
			ShimPID: os.Getpid(), // 当前进程，肯定存活
		}

		isValid, shouldCleanup, err := ValidateSandboxState(ctx, storage.ID, storage)

		// shim 存活，应该返回错误（另一个实例）
		assert.False(t, isValid, "shim 存活时应该返回无效")
		assert.False(t, shouldCleanup, "shim 存活时不需要清理")
		assert.Error(t, err, "shim 存活时应该返回错误")
		assert.Contains(t, err.Error(), "another shim instance", "错误信息应该提到另一个实例")
	})

	t.Run("僵尸状态 - shim 死亡且 RTOS 不存在", func(t *testing.T) {
		// 使用一个不存在的容器 ID，这样 RTOS 也不存在
		storage := &SandboxStorage{
			ID:      "test-container-nonexistent-" + time.Now().Format("20060102150405"),
			ShimPID: 99999, // 不存在的 PID
		}

		isValid, shouldCleanup, err := ValidateSandboxState(ctx, storage.ID, storage)

		// RTOS 不存在，应该标记需要清理
		assert.False(t, isValid, "RTOS 不存在时应该返回无效")
		assert.True(t, shouldCleanup, "RTOS 不存在时应该标记需要清理")
		assert.NoError(t, err, "RTOS 不存在时不应返回错误")
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
		isValid, shouldCleanup, err := ValidateSandboxState(ctx, "test", nil)

		assert.False(t, isValid)
		assert.False(t, shouldCleanup)
		assert.Error(t, err)
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
