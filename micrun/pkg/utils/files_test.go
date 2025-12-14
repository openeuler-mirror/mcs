//go:build test
// +build test

package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DummySandboxState represents a simplified sandbox state for testing
type DummySandboxState struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	CPUCount  int       `json:"cpu_count"`
	MemoryMB  int       `json:"memory_mb"`
}

// DummyContainerState represents a simplified container state for testing
type DummyContainerState struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Image      string            `json:"image"`
	State      string            `json:"state"`
	ExitCode   int               `json:"exit_code"`
	Labels     map[string]string `json:"labels"`
	StartedAt  time.Time         `json:"started_at"`
	FinishedAt time.Time         `json:"finished_at"`
}

// MockSandboxState represents the actual sandbox state structure from micantainer package
type MockSandboxState struct {
	State   string `json:"state"`
	Ped     string `json:"ped"`
	Version uint   `json:"version"`
}

// MockContainerState represents the actual container state structure from micantainer package
type MockContainerState struct {
	Bundle string `json:"bundle"`
	ID     string `json:"id"`
	CType  string `json:"ctype"`
	State  string `json:"state"`
}

// MockNetworkConfig represents the network configuration structure
type MockNetworkConfig struct {
	NetworkID      string `json:"network_id"`
	NetworkCreated bool   `json:"network_created"`
}

// MockSandboxStorage represents the sandbox storage structure from micantainer package
type MockSandboxStorage struct {
	ID      string            `json:"id"`
	State   string            `json:"state"`
	Network MockNetworkConfig `json:"network"`
	Version uint              `json:"version"`
}

func TestRestoreStructFromFile(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "micran_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	t.Run("should restore sandbox state correctly", func(t *testing.T) {
		// Create test sandbox state
		testSandbox := DummySandboxState{
			ID:        "test-sandbox-123",
			Status:    "running",
			CreatedAt: time.Now().UTC(),
			CPUCount:  4,
			MemoryMB:  2048,
		}

		// Save to file
		sandboxFile := filepath.Join(tempDir, "sandbox_state.json")
		err := SaveStructToJSON(sandboxFile, testSandbox)
		require.NoError(t, err)

		// Restore from file
		restored, err := RestoreStructFromJSON(sandboxFile)
		require.NoError(t, err)

		// Type assertion and verification
		restoredMap, ok := restored.(map[string]interface{})
		require.True(t, ok, "Restored value should be a map")

		assert.Equal(t, testSandbox.ID, restoredMap["id"])
		assert.Equal(t, testSandbox.Status, restoredMap["status"])
		assert.Equal(t, float64(testSandbox.CPUCount), restoredMap["cpu_count"])
		assert.Equal(t, float64(testSandbox.MemoryMB), restoredMap["memory_mb"])
	})

	t.Run("should restore container state correctly", func(t *testing.T) {
		// Create test container state
		testContainer := DummyContainerState{
			ID:        "test-container-456",
			Name:      "test-app",
			Image:     "ubuntu:20.04",
			State:     "exited",
			ExitCode:  0,
			Labels:    map[string]string{"env": "test", "team": "backend"},
			StartedAt: time.Now().UTC().Add(-time.Hour),
		}

		// Save to file
		containerFile := filepath.Join(tempDir, "container_state.json")
		err := SaveStructToJSON(containerFile, testContainer)
		require.NoError(t, err)

		// Restore from file
		restored, err := RestoreStructFromJSON(containerFile)
		require.NoError(t, err)

		// Type assertion and verification
		restoredMap, ok := restored.(map[string]interface{})
		require.True(t, ok, "Restored value should be a map")

		assert.Equal(t, testContainer.ID, restoredMap["id"])
		assert.Equal(t, testContainer.Name, restoredMap["name"])
		assert.Equal(t, testContainer.Image, restoredMap["image"])
		assert.Equal(t, testContainer.State, restoredMap["state"])
		assert.Equal(t, float64(testContainer.ExitCode), restoredMap["exit_code"])

		// Verify labels
		labels, ok := restoredMap["labels"].(map[string]interface{})
		require.True(t, ok, "Labels should be a map")
		assert.Equal(t, testContainer.Labels["env"], labels["env"])
		assert.Equal(t, testContainer.Labels["team"], labels["team"])
	})

	t.Run("should handle non-existent file", func(t *testing.T) {
		nonExistentFile := filepath.Join(tempDir, "non_existent.json")
		_, err := RestoreStructFromJSON(nonExistentFile)
		require.Error(t, err)
		assert.True(t, os.IsNotExist(err), "error should be a not exist error")
	})

	t.Run("should handle invalid JSON file", func(t *testing.T) {
		invalidFile := filepath.Join(tempDir, "invalid.json")
		err := os.WriteFile(invalidFile, []byte("{invalid json}"), 0644)
		require.NoError(t, err)

		_, err = RestoreStructFromJSON(invalidFile)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal JSON")
	})
}

func TestSaveAndRestoreIntegration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "micran_integration_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Test with actual JSON marshaling/unmarshaling to ensure compatibility
	original := map[string]interface{}{
		"string_field": "test_value",
		"number_field": 42,
		"bool_field":   true,
		"array_field":  []string{"item1", "item2"},
		"object_field": map[string]interface{}{
			"nested": "value",
		},
	}

	testFile := filepath.Join(tempDir, "integration_test.json")

	// Save using standard json.Marshal to ensure format
	jsonBytes, err := json.Marshal(original)
	require.NoError(t, err)
	err = os.WriteFile(testFile, jsonBytes, 0644)
	require.NoError(t, err)

	// Restore using our function
	restored, err := RestoreStructFromJSON(testFile)
	require.NoError(t, err)

	// Verify the restored structure matches original
	restoredMap, ok := restored.(map[string]interface{})
	require.True(t, ok)

	assert.Equal(t, original["string_field"], restoredMap["string_field"])
	// JSON unmarshaling to interface{} converts numbers to float64, so we need to compare accordingly
	assert.InDelta(t, float64(original["number_field"].(int)), restoredMap["number_field"].(float64), 0.001)
	assert.Equal(t, original["bool_field"], restoredMap["bool_field"])

	// Verify array
	restoredArray, ok := restoredMap["array_field"].([]interface{})
	require.True(t, ok)
	assert.ElementsMatch(t, original["array_field"].([]string), []string{
		restoredArray[0].(string),
		restoredArray[1].(string),
	})

	// Verify nested object
	restoredObject, ok := restoredMap["object_field"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, original["object_field"].(map[string]interface{})["nested"], restoredObject["nested"])
}

func TestStoreAndRestoreMockSandboxState(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "micran_mock_sandbox_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test mock sandbox state
	testSandbox := MockSandboxState{
		State:   "running",
		Ped:     "xen",
		Version: 1,
	}

	// Save to file
	sandboxFile := filepath.Join(tempDir, "mock_sandbox_state.json")
	err = SaveStructToJSON(sandboxFile, testSandbox)
	require.NoError(t, err)

	// Restore from file
	restored, err := RestoreStructFromJSON(sandboxFile)
	require.NoError(t, err)

	// Type assertion and verification
	restoredMap, ok := restored.(map[string]interface{})
	require.True(t, ok, "Restored value should be a map")

	assert.Equal(t, testSandbox.State, restoredMap["state"])
	assert.Equal(t, testSandbox.Ped, restoredMap["ped"])
	assert.Equal(t, float64(testSandbox.Version), restoredMap["version"])
}

func TestStoreAndRestoreMockContainerState(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "micran_mock_container_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test mock container state
	testContainer := MockContainerState{
		Bundle: "/test/bundle",
		ID:     "test-container-789",
		CType:  "single_container",
		State:  "ready",
	}

	// Save to file
	containerFile := filepath.Join(tempDir, "mock_container_state.json")
	err = SaveStructToJSON(containerFile, testContainer)
	require.NoError(t, err)

	// Restore from file
	restored, err := RestoreStructFromJSON(containerFile)
	require.NoError(t, err)

	// Type assertion and verification
	restoredMap, ok := restored.(map[string]interface{})
	require.True(t, ok, "Restored value should be a map")

	assert.Equal(t, testContainer.Bundle, restoredMap["bundle"])
	assert.Equal(t, testContainer.ID, restoredMap["id"])
	assert.Equal(t, testContainer.CType, restoredMap["ctype"])
	assert.Equal(t, testContainer.State, restoredMap["state"])
}

func TestStoreAndRestoreMockSandboxStorage(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "micran_mock_sandbox_storage_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test mock sandbox storage
	testSandboxStorage := MockSandboxStorage{
		ID:    "test-sandbox-123",
		State: "running",
		Network: MockNetworkConfig{
			NetworkID:      "net-456",
			NetworkCreated: true,
		},
		Version: 1,
	}

	// Save to file
	sandboxFile := filepath.Join(tempDir, "mock_sandbox_storage.json")
	err = SaveStructToJSON(sandboxFile, testSandboxStorage)
	require.NoError(t, err)

	// Restore from file
	restored, err := RestoreStructFromJSON(sandboxFile)
	require.NoError(t, err)

	// Type assertion and verification
	restoredMap, ok := restored.(map[string]interface{})
	require.True(t, ok, "Restored value should be a map")

	assert.Equal(t, testSandboxStorage.ID, restoredMap["id"])
	assert.Equal(t, testSandboxStorage.State, restoredMap["state"])
	assert.Equal(t, float64(testSandboxStorage.Version), restoredMap["version"])

	// Verify network config
	network, ok := restoredMap["network"].(map[string]interface{})
	require.True(t, ok, "Network should be a map")
	assert.Equal(t, testSandboxStorage.Network.NetworkID, network["network_id"])
	assert.Equal(t, testSandboxStorage.Network.NetworkCreated, network["network_created"])
}

func TestTravelDir(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "traveldir_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create a nested directory structure
	dir1 := filepath.Join(tempDir, "dir1")
	dir2 := filepath.Join(dir1, "dir2")
	err = os.MkdirAll(dir2, 0755)
	require.NoError(t, err)

	// Create some files
	_, err = os.Create(filepath.Join(dir1, "file1.txt"))
	require.NoError(t, err)
	_, err = os.Create(filepath.Join(dir2, "file2.txt"))
	require.NoError(t, err)
	_, err = os.Create(filepath.Join(tempDir, "file3.txt"))
	require.NoError(t, err)

	// The TravelDir function logs to debug, so we can't easily capture output.
	// We'll just test that it runs without error on a valid directory.
	err = TravelDir(tempDir)
	assert.NoError(t, err)

	// Test with a non-existent directory
	err = TravelDir(filepath.Join(tempDir, "non_existent_dir"))
	assert.Error(t, err)

	// Test with a symbolic link to the current directory to check for infinite loops
	symlinkPath := filepath.Join(dir2, "loop_link")
	err = os.Symlink(".", symlinkPath)
	require.NoError(t, err)

	// TravelDir should not follow the symlink and should complete without error.
	err = TravelDir(tempDir)
	assert.NoError(t, err, "TravelDir should not get stuck in a symlink loop")
}
