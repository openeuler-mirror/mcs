package libmica

import (
	"bytes"
	"context"
	"encoding/binary"
	"micrun/internal/ports"
	defs "micrun/internal/support/definitions"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestClientSocketPath(t *testing.T) {
	got := clientSocketPath("demo")
	want := filepath.Join(defs.MicaStateDir, "demo.socket")
	if got != want {
		t.Fatalf("clientSocketPath() = %q, want %q", got, want)
	}
}

func TestParseMicaStatus(t *testing.T) {
	tests := []struct {
		name        string
		response    string
		wantStatus  *MicaStatus
		wantErr     bool
		errContains string
	}{
		{
			name:     "valid single core",
			response: "zephyr                        0                  Running             pty rpc umt",
			wantStatus: &MicaStatus{
				Name:     "zephyr",
				CPU:      "0",
				State:    running,
				Services: []MicaService{servicePTY, serviceRPC, serviceUMT},
				Raw:      "zephyr                        0                  Running             pty rpc umt",
			},
			wantErr: false,
		},
		{
			name:     "valid multi-core range",
			response: "zephyr                        0-3                Running             pty rpc umt",
			wantStatus: &MicaStatus{
				Name:     "zephyr",
				CPU:      "0-3",
				State:    running,
				Services: []MicaService{servicePTY, serviceRPC, serviceUMT},
				Raw:      "zephyr                        0-3                Running             pty rpc umt",
			},
			wantErr: false,
		},
		{
			name:     "valid multi-core complex",
			response: "zephyr                        1-3,5              Running             pty rpc umt",
			wantStatus: &MicaStatus{
				Name:     "zephyr",
				CPU:      "1-3,5",
				State:    running,
				Services: []MicaService{servicePTY, serviceRPC, serviceUMT},
				Raw:      "zephyr                        1-3,5              Running             pty rpc umt",
			},
			wantErr: false,
		},
		{
			name:     "valid multi-core comma separated",
			response: "zephyr                        0,2,4              Running             pty rpc umt",
			wantStatus: &MicaStatus{
				Name:     "zephyr",
				CPU:      "0,2,4",
				State:    running,
				Services: []MicaService{servicePTY, serviceRPC, serviceUMT},
				Raw:      "zephyr                        0,2,4              Running             pty rpc umt",
			},
			wantErr: false,
		},
		{
			name:     "valid CPU canonicalized",
			response: "zephyr                        2,1,1              Running             pty rpc umt",
			wantStatus: &MicaStatus{
				Name:     "zephyr",
				CPU:      "1-2",
				State:    running,
				Services: []MicaService{servicePTY, serviceRPC, serviceUMT},
				Raw:      "zephyr                        2,1,1              Running             pty rpc umt",
			},
			wantErr: false,
		},
		{
			name:     "valid error state",
			response: "zephyr                        0                  Error               pty rpc",
			wantStatus: &MicaStatus{
				Name:     "zephyr",
				CPU:      "0",
				State:    stateErr,
				Services: []MicaService{servicePTY, serviceRPC},
				Raw:      "zephyr                        0                  Error               pty rpc",
			},
			wantErr: false,
		},
		{
			name:        "invalid CPU format",
			response:    "zephyr                        invalid            Running             pty rpc umt",
			wantErr:     true,
			errContains: "invalid CPU field format",
		},
		{
			name:        "invalid CPU range",
			response:    "zephyr                        3-1                Running             pty rpc umt",
			wantErr:     true,
			errContains: "invalid CPU field format",
		},
		{
			name:        "invalid CPU range",
			response:    "zephyr                        13-3                Running             pty rpc umt",
			wantErr:     true,
			errContains: "invalid CPU field format",
		},
		{
			name:        "empty response",
			response:    "",
			wantErr:     true,
			errContains: "empty response",
		},
		{
			name:        "error response",
			response:    "MICA-FAILED",
			wantErr:     true,
			errContains: "error response",
		},
		{
			name:        "insufficient fields",
			response:    "zephyr 1",
			wantErr:     true,
			errContains: "invalid status format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStatus, err := parseMicaStatus(tt.response)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseMicaStatus() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("parseMicaStatus() error = %v, want contains %v", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("parseMicaStatus() unexpected error = %v", err)
				return
			}

			if !reflect.DeepEqual(gotStatus, tt.wantStatus) {
				t.Errorf("parseMicaStatus() = %v, want %v", gotStatus, tt.wantStatus)
			}
		})
	}
}

func TestSplitMicaStatusFields(t *testing.T) {
	got, err := splitMicaStatusFields("zephyr 0-3 Running pty rpc")
	if err != nil {
		t.Fatalf("splitMicaStatusFields returned error: %v", err)
	}
	if got.name != "zephyr" || got.cpu != "0-3" || got.state != "Running" {
		t.Fatalf("unexpected status fields: %+v", got)
	}
	if !reflect.DeepEqual(got.services, []string{"pty", "rpc"}) {
		t.Fatalf("unexpected services: %v", got.services)
	}
}

func TestParseMicaStatusWithEmptyCPUUsesProviderContext(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "status")
	called := false

	got, err := parseMicaStatusWithCPUProvider(ctx, "zephyr Running pty rpc", func(got context.Context) int {
		called = true
		if got.Value(contextKey{}) != "status" {
			t.Fatalf("max CPU provider did not receive caller context")
		}
		return 4
	})
	if err != nil {
		t.Fatalf("parseMicaStatusWithCPUProvider returned error: %v", err)
	}
	if !called {
		t.Fatal("expected max CPU provider to be called")
	}
	if got.CPU != "0-3" || got.State != running {
		t.Fatalf("unexpected parsed status: %+v", got)
	}
	if !reflect.DeepEqual(got.Services, []MicaService{servicePTY, serviceRPC}) {
		t.Fatalf("unexpected services: %v", got.Services)
	}
}

func TestParseMicaStatusAcceptsStateOnlyResponse(t *testing.T) {
	got, err := parseMicaStatusWithCPUProvider(context.Background(), "zephyr Offline", func(context.Context) int {
		return 4
	})
	if err != nil {
		t.Fatalf("parseMicaStatusWithCPUProvider returned error: %v", err)
	}
	if got.Name != "zephyr" || got.CPU != "0-3" || got.State != offline {
		t.Fatalf("unexpected parsed status: %+v", got)
	}
	if len(got.Services) != 0 {
		t.Fatalf("unexpected services: %v", got.Services)
	}
}

func TestIsValidCPUString(t *testing.T) {
	tests := []struct {
		name     string
		cpuStr   string
		expected bool
	}{
		{"single core", "0", true},
		{"single core one", "1", true},
		{"range", "0-3", true},
		{"complex range", "1-3,5", true},
		{"comma separated", "0,2,4", true},
		{"multiple ranges", "0-1,3-5,7", true},
		{"spaces with trim", "1 - 3", true},
		{"empty", "", true}, // Empty string is now valid (means all CPUs)
		{"invalid range", "3-1", false},
		{"invalid range single", "1-", false},
		{"invalid range dash", "-3", false},
		{"non numeric", "abc", false},
		{"partial invalid", "1,abc,3", false},
		{"partial invalid range", "1-abc", false},
		{"comma only", ",", false},
		{"trailing comma", "1,3,", false},
		{"leading comma", ",1,3", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidCPUString(tt.cpuStr)
			if result != tt.expected {
				t.Errorf("isValidCPUString(%q) = %v, want %v", tt.cpuStr, result, tt.expected)
			}
		})
	}
}

func TestParseCPUString(t *testing.T) {
	tests := []struct {
		name       string
		cpuStr     string
		wantCPUs   []int
		wantErr    bool
		errContain string
	}{
		{"single core", "0", []int{0}, false, ""},
		{"range", "0-3", []int{0, 1, 2, 3}, false, ""},
		{"complex", "1-3,5", []int{1, 2, 3, 5}, false, ""},
		{"comma separated", "0,2,4", []int{0, 2, 4}, false, ""},
		{"multiple ranges", "0-1,3-5,7", []int{0, 1, 3, 4, 5, 7}, false, ""},
		{"single range", "3-3", []int{3}, false, ""},
		{"invalid", "invalid", nil, true, "invalid CPU string format"},
		{"invalid range", "3-1", nil, true, "invalid CPU string format"},
		{"empty", "", []int{}, false, ""}, // Empty string is valid, returns empty slice
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCPUs, err := ParseCPUString(tt.cpuStr)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseCPUString() expected error, got nil")
					return
				}
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("ParseCPUString() error = %v, want contain %v", err, tt.errContain)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseCPUString() unexpected error = %v", err)
				return
			}

			if len(gotCPUs) != len(tt.wantCPUs) {
				t.Errorf("ParseCPUString() length = %d, want %d", len(gotCPUs), len(tt.wantCPUs))
			}
			for i := range gotCPUs {
				if gotCPUs[i] != tt.wantCPUs[i] {
					t.Errorf("ParseCPUString()[%d] = %d, want %d", i, gotCPUs[i], tt.wantCPUs[i])
				}
			}
		})
	}
}

func TestMicaStatusMethods(t *testing.T) {
	status := &MicaStatus{
		Name:     "test",
		CPU:      "0-3,5",
		State:    running,
		Services: []MicaService{servicePTY, serviceRPC},
	}

	t.Run("String()", func(t *testing.T) {
		result := status.String()
		expected := "Name: test, CPU: 0-3,5, State: Running, Services: [pty rpc]"
		if result != expected {
			t.Errorf("MicaStatus.String() = %q, want %q", result, expected)
		}
	})

	t.Run("isValid()", func(t *testing.T) {
		result := status.isValid()
		if !result {
			t.Errorf("MicaStatus.isValid() = %v, want true", result)
		}
	})

	t.Run("isValid with empty name", func(t *testing.T) {
		invalidStatus := *status
		invalidStatus.Name = ""
		result := invalidStatus.isValid()
		if result {
			t.Errorf("MicaStatus.isValid() with empty name = %v, want false", result)
		}
	})

	t.Run("isValid with invalid CPU", func(t *testing.T) {
		invalidStatus := *status
		invalidStatus.CPU = "invalid"
		result := invalidStatus.isValid()
		if result {
			t.Errorf("MicaStatus.isValid() with invalid CPU = %v, want false", result)
		}
	})

	t.Run("isValid with unknown state", func(t *testing.T) {
		invalidStatus := *status
		invalidStatus.State = unknown
		result := invalidStatus.isValid()
		if result {
			t.Errorf("MicaStatus.isValid() with unknown state = %v, want false", result)
		}
	})
}

func TestParseMicaSocketResponse(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		want        *micaSocketResponse
		wantErr     bool
		errContains string
	}{
		{
			name:  "success with payload",
			input: "demo                          0-1                Running             pty rpcMICA-SUCCESS",
			want: &micaSocketResponse{
				payload: "demo                          0-1                Running             pty rpc",
				status:  defs.MicaSuccess,
			},
		},
		{
			name:  "success only",
			input: "MICA-SUCCESS",
			want: &micaSocketResponse{
				payload: "",
				status:  defs.MicaSuccess,
			},
		},
		{
			name:  "failure with payload",
			input: "something bad happenedMICA-FAILED",
			want: &micaSocketResponse{
				payload: "something bad happened",
				status:  defs.MicaFailed,
			},
		},
		{
			name:        "invalid response",
			input:       "plain output without markers",
			wantErr:     true,
			errContains: "unexpected response format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMicaSocketResponse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("parseMicaSocketResponse() expected error, got nil")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("parseMicaSocketResponse() error = %v, want contains %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseMicaSocketResponse() unexpected error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseMicaSocketResponse() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestMicaSocketResponsePayloadOrError(t *testing.T) {
	success := &micaSocketResponse{payload: "payload", status: defs.MicaSuccess}
	got, err := success.payloadOrError()
	if err != nil {
		t.Fatalf("payloadOrError() unexpected error = %v", err)
	}
	if got != "payload" {
		t.Fatalf("payloadOrError() = %q, want %q", got, "payload")
	}

	failed := &micaSocketResponse{payload: "bad", status: defs.MicaFailed}
	if _, err := failed.payloadOrError(); err == nil || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("payloadOrError() failure error = %v, want payload detail", err)
	}

	if err := (*micaSocketResponse)(nil).err(); err != ErrUnexpectedRespFormat {
		t.Fatalf("nil response err() = %v, want %v", err, ErrUnexpectedRespFormat)
	}
}

func TestMicaExecutor_ReadResource(t *testing.T) {
	tests := []struct {
		name     string
		executor MicaExecutor
		want     *ports.ResourceSnapshot
	}{
		{
			name: "all fields populated",
			executor: MicaExecutor{
				records: MicaClientConf{
					vcpuNum:     4,
					cpuWeight:   1024,
					cpuCapacity: 50,
					memoryMB:    256,
					cpuStr:      [MaxCPUStringLen]byte{'0', '-', '3', ',', '5'},
				},
				ID: "test-container",
			},
			want: &ports.ResourceSnapshot{
				CPUCapacity:  func() *uint32 { v := uint32(50); return &v }(),
				VCPU:         func() *uint32 { v := uint32(4); return &v }(),
				CPUWeight:    func() *uint32 { v := uint32(1024); return &v }(),
				MemoryMaxMB:  func() *uint32 { v := uint32(256); return &v }(),
				ClientCPUSet: "0-3,5",
			},
		},
		{
			name: "zero values - no pointers set",
			executor: MicaExecutor{
				records: MicaClientConf{
					vcpuNum:     0,
					cpuWeight:   0,
					cpuCapacity: 0,
					memoryMB:    0,
					cpuStr:      [MaxCPUStringLen]byte{},
				},
				ID: "test-container-zero",
			},
			want: &ports.ResourceSnapshot{
				ClientCPUSet: "",
			},
		},
		{
			name: "partial fields - only some pointers set",
			executor: MicaExecutor{
				records: MicaClientConf{
					vcpuNum:     2,
					cpuWeight:   0, // zero, so not set
					cpuCapacity: 75,
					memoryMB:    0, // zero, so not set
					cpuStr:      [MaxCPUStringLen]byte{'1', ',', '2'},
				},
				ID: "test-container-partial",
			},
			want: &ports.ResourceSnapshot{
				CPUCapacity:  func() *uint32 { v := uint32(75); return &v }(),
				VCPU:         func() *uint32 { v := uint32(2); return &v }(),
				CPUWeight:    nil,
				MemoryMaxMB:  nil,
				ClientCPUSet: "1,2",
			},
		},
		{
			name: "cpu string with null bytes",
			executor: MicaExecutor{
				records: MicaClientConf{
					vcpuNum:     1,
					cpuWeight:   512,
					cpuCapacity: 25,
					memoryMB:    128,
					cpuStr:      [MaxCPUStringLen]byte{'0', '-', '7', 0, 0, 0}, // with null bytes
				},
				ID: "test-container-null",
			},
			want: &ports.ResourceSnapshot{
				CPUCapacity:  func() *uint32 { v := uint32(25); return &v }(),
				VCPU:         func() *uint32 { v := uint32(1); return &v }(),
				CPUWeight:    func() *uint32 { v := uint32(512); return &v }(),
				MemoryMaxMB:  func() *uint32 { v := uint32(128); return &v }(),
				ClientCPUSet: "0-7",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.executor.ReadResource()

			if got.CPUCapacity == nil || tt.want.CPUCapacity == nil {
				if got.CPUCapacity != tt.want.CPUCapacity {
					t.Errorf("ReadResource().CPUCapacity = %v, want %v", got.CPUCapacity, tt.want.CPUCapacity)
				}
			} else if *got.CPUCapacity != *tt.want.CPUCapacity {
				t.Errorf("ReadResource().CPUCapacity = %v, want %v", *got.CPUCapacity, *tt.want.CPUCapacity)
			}

			if got.VCPU == nil || tt.want.VCPU == nil {
				if got.VCPU != tt.want.VCPU {
					t.Errorf("ReadResource().VCPU = %v, want %v", got.VCPU, tt.want.VCPU)
				}
			} else if *got.VCPU != *tt.want.VCPU {
				t.Errorf("ReadResource().VCPU = %v, want %v", *got.VCPU, *tt.want.VCPU)
			}

			if got.CPUWeight == nil || tt.want.CPUWeight == nil {
				if got.CPUWeight != tt.want.CPUWeight {
					t.Errorf("ReadResource().CPUWeight = %v, want %v", got.CPUWeight, tt.want.CPUWeight)
				}
			} else if *got.CPUWeight != *tt.want.CPUWeight {
				t.Errorf("ReadResource().CPUWeight = %v, want %v", *got.CPUWeight, *tt.want.CPUWeight)
			}

			if got.MemoryMaxMB == nil || tt.want.MemoryMaxMB == nil {
				if got.MemoryMaxMB != tt.want.MemoryMaxMB {
					t.Errorf("ReadResource().MemoryMaxMB = %v, want %v", got.MemoryMaxMB, tt.want.MemoryMaxMB)
				}
			} else if *got.MemoryMaxMB != *tt.want.MemoryMaxMB {
				t.Errorf("ReadResource().MemoryMaxMB = %v, want %v", *got.MemoryMaxMB, *tt.want.MemoryMaxMB)
			}

			if got.ClientCPUSet != tt.want.ClientCPUSet {
				t.Errorf("ReadResource().ClientCPUSet = %v, want %v", got.ClientCPUSet, tt.want.ClientCPUSet)
			}
		})
	}
}

func TestMicaExecutor_MemoryTracking(t *testing.T) {
	exec := &MicaExecutor{ID: "mem-test"}

	exec.RecordMemoryState(64, 64)
	if got := exec.records.memoryMB; got != 64 {
		t.Fatalf("records.memoryMB = %d, want 64", got)
	}
	if got := exec.MemoryThresholdMB(); got != 64 {
		t.Fatalf("MemoryThresholdMB() = %d, want 64", got)
	}

	exec.RecordMemoryState(32, 128)
	if got := exec.records.memoryMB; got != 32 {
		t.Fatalf("records.memoryMB after update = %d, want 32", got)
	}
	if got := exec.MemoryThresholdMB(); got != 128 {
		t.Fatalf("MemoryThresholdMB() after update = %d, want 128", got)
	}

	exec.RecordMemoryState(32, 0)
	if got := exec.MemoryThresholdMB(); got != 128 {
		t.Fatalf("MemoryThresholdMB() after zero threshold = %d, want 128", got)
	}

	if exec.NeedUpdateMemLimit(32) {
		t.Fatalf("NeedUpdateMemLimit should be false when limits match")
	}
	if !exec.NeedUpdateMemLimit(64) {
		t.Fatalf("NeedUpdateMemLimit should be true when growing memory")
	}
	if !exec.NeedUpdateMemLimit(16) {
		t.Fatalf("NeedUpdateMemLimit should be true when shrinking memory")
	}
}

func TestBoundaryConditions(t *testing.T) {
	// Test CPU string boundary conditions
	t.Run("CPU string boundary conditions", func(t *testing.T) {
		tests := []struct {
			name     string
			cpuStr   string
			expected bool
		}{
			{"zero only", "0", true},
			{"zero range", "0-0", true},
			{"large zero range", "0-63", true},
			{"mixed zero", "0,1,2", true},
			{"zero with others", "0-2,5-7", true},
			{"zero with spaces", " 0 ", true},
			{"single digit max", "9", true},
			{"double digit", "10", true},
			{"double digit range", "10-15", true},
			{"large range", "0-255", true},
			{"negative start", "-1-3", false},
			{"negative single", "-1", false},
			{"zero with negative", "0,-1,2", false},
			{"edge case range", "1-0", false}, // invalid range
			{"empty group", "0,,1", false},
			{"whitespace only", "   ", false},
			{"dash only", "-", false},
			{"comma dash", "0,-,1", false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := isValidCPUString(tt.cpuStr)
				if result != tt.expected {
					t.Errorf("isValidCPUString(%q) = %v, want %v", tt.cpuStr, result, tt.expected)
				}
			})
		}
	})

	// Test ParseCPUString boundary conditions
	t.Run("ParseCPUString boundary conditions", func(t *testing.T) {
		tests := []struct {
			name     string
			cpuStr   string
			wantCPUs []int
			wantErr  bool
		}{
			{"zero only", "0", []int{0}, false},
			{"zero range", "0-0", []int{0}, false},
			{"large zero range", "0-3", []int{0, 1, 2, 3}, false},
			{"single zero", "0", []int{0}, false},
			{"multiple zeros", "0,0,0", []int{0}, false},
			{"mixed zero", "0,1,2", []int{0, 1, 2}, false},
			{"zero with gaps", "0,2,4", []int{0, 2, 4}, false},
			{"large number", "255", []int{255}, false},
			{"large range", "250-255", []int{250, 251, 252, 253, 254, 255}, false},
			{"complex large", "0-1,250-255", []int{0, 1, 250, 251, 252, 253, 254, 255}, false},
			{"negative start", "-1", nil, true},
			{"negative range", "-1-3", nil, true},
			{"invalid range", "3-1", nil, true},
			{"empty group", "0,,1", nil, true},
			{"single comma", ",", nil, true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				gotCPUs, err := ParseCPUString(tt.cpuStr)

				if tt.wantErr {
					if err == nil {
						t.Errorf("ParseCPUString() expected error, got nil")
						return
					}
					return
				}

				if err != nil {
					t.Errorf("ParseCPUString() unexpected error = %v", err)
					return
				}

				if !reflect.DeepEqual(gotCPUs, tt.wantCPUs) {
					t.Errorf("ParseCPUString() = %v, want %v", gotCPUs, tt.wantCPUs)
				}
			})
		}
	})

	// Test MicaStatus parsing boundary conditions
	t.Run("MicaStatus parsing boundary conditions", func(t *testing.T) {
		tests := []struct {
			name        string
			response    string
			wantStatus  *MicaStatus
			wantErr     bool
			errContains string
		}{
			{
				name:     "CPU zero only",
				response: "zephyr                        0                  Running             pty rpc umt",
				wantStatus: &MicaStatus{
					Name:     "zephyr",
					CPU:      "0",
					State:    running,
					Services: []MicaService{servicePTY, serviceRPC, serviceUMT},
					Raw:      "zephyr                        0                  Running             pty rpc umt",
				},
				wantErr: false,
			},
			{
				name:     "CPU zero range",
				response: "zephyr                        0-3                Running             pty rpc umt",
				wantStatus: &MicaStatus{
					Name:     "zephyr",
					CPU:      "0-3",
					State:    running,
					Services: []MicaService{servicePTY, serviceRPC, serviceUMT},
					Raw:      "zephyr                        0-3                Running             pty rpc umt",
				},
				wantErr: false,
			},
			{
				name:     "CPU large range",
				response: "zephyr                        0-63               Running             pty rpc umt",
				wantStatus: &MicaStatus{
					Name:     "zephyr",
					CPU:      "0-63",
					State:    running,
					Services: []MicaService{servicePTY, serviceRPC, serviceUMT},
					Raw:      "zephyr                        0-63               Running             pty rpc umt",
				},
				wantErr: false,
			},
			{
				name:     "CPU complex zero",
				response: "zephyr                        0,2,4              Running             pty rpc umt",
				wantStatus: &MicaStatus{
					Name:     "zephyr",
					CPU:      "0,2,4",
					State:    running,
					Services: []MicaService{servicePTY, serviceRPC, serviceUMT},
					Raw:      "zephyr                        0,2,4              Running             pty rpc umt",
				},
				wantErr: false,
			},
			{
				name:        "CPU negative",
				response:    "zephyr                        -1                 Running             pty rpc umt",
				wantErr:     true,
				errContains: "invalid CPU field format",
			},
			{
				name:        "CPU invalid range",
				response:    "zephyr                        3-0                Running             pty rpc umt",
				wantErr:     true,
				errContains: "invalid CPU field format",
			},
			{
				name:        "CPU empty field",
				response:    "zephyr                                           Running             pty rpc umt",
				wantErr:     true,
				errContains: "failed to get max CPU number",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				gotStatus, err := parseMicaStatus(tt.response)

				if tt.wantErr {
					if err == nil {
						t.Errorf("parseMicaStatus() expected error, got nil")
						return
					}
					if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
						t.Errorf("parseMicaStatus() error = %v, want contains %v", err, tt.errContains)
					}
					return
				}

				if err != nil {
					t.Errorf("parseMicaStatus() unexpected error = %v", err)
					return
				}

				if !reflect.DeepEqual(gotStatus, tt.wantStatus) {
					t.Errorf("parseMicaStatus() = %v, want %v", gotStatus, tt.wantStatus)
				}
			})
		}
	})

	// Test state parsing boundary conditions
	t.Run("State parsing boundary conditions", func(t *testing.T) {
		tests := []struct {
			name      string
			stateStr  string
			wantState MicaState
		}{
			{"empty state", "", unknown},
			{"unknown state", "UnknownState", unknown},
			{"mixed case", "running", unknown},
			{"uppercase", "RUNNING", unknown},
			{"partial match", "Runn", unknown},
			{"with spaces", " Running ", unknown},
			{"valid offline", "Offline", offline},
			{"valid configured", "Configured", configured},
			{"valid ready", "Ready", ready},
			{"valid running", "Running", running},
			{"valid suspended", "Suspended", suspended},
			{"valid stopped", "Stopped", stopped},
			{"valid error", "Error", stateErr},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				gotState := parseMicaState(tt.stateStr)
				if gotState != tt.wantState {
					t.Errorf("parseMicaState(%q) = %v, want %v", tt.stateStr, gotState, tt.wantState)
				}
			})
		}
	})

	// Test service parsing boundary conditions
	t.Run("Service parsing boundary conditions", func(t *testing.T) {
		tests := []struct {
			name         string
			fields       []string
			wantServices []MicaService
		}{
			{"empty services", []string{}, []MicaService{}},
			{"single pty", []string{"pty"}, []MicaService{servicePTY}},
			{"single rpc", []string{"rpc"}, []MicaService{serviceRPC}},
			{"single umt", []string{"umt"}, []MicaService{serviceUMT}},
			{"single debug", []string{"debug"}, []MicaService{serviceDebug}},
			{"mixed case", []string{"PTY", "RPC"}, []MicaService{servicePTY, serviceRPC}},
			{"partial match", []string{"ptytest"}, []MicaService{servicePTY}},
			{"multiple services", []string{"pty", "rpc", "umt", "debug"}, []MicaService{servicePTY, serviceRPC, serviceUMT, serviceDebug}},
			{"with spaces", []string{" pty ", " rpc "}, []MicaService{servicePTY, serviceRPC}},
			{"unknown service", []string{"unknown"}, []MicaService{}},
			{"mixed known unknown", []string{"pty", "unknown", "rpc"}, []MicaService{servicePTY, serviceRPC}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				gotServices := parseMicaServices(tt.fields)
				if len(gotServices) != len(tt.wantServices) {
					t.Errorf("parseMicaServices(%v) length = %d, want %d", tt.fields, len(gotServices), len(tt.wantServices))
				}
				for i := range gotServices {
					if gotServices[i] != tt.wantServices[i] {
						t.Errorf("parseMicaServices(%v)[%d] = %v, want %v", tt.fields, i, gotServices[i], tt.wantServices[i])
					}
				}
			})
		}
	})
}

func TestMicaClientConfPackIncludesMaxFields(t *testing.T) {
	opts := MicaClientConfCreateOptions{
		CPU:             "0-1",
		VCPUs:           2,
		MaxVCPUs:        5,
		CPUWeight:       1024,
		CPUCapacity:     100,
		MemoryMB:        128,
		MemoryThreshold: 256,
		Network:         "net123",
	}
	conf := MicaClientConf{}
	conf.InitWithOpts(opts)

	buf := conf.pack()
	if got := len(buf); got != createMsgSerializedBufSize {
		t.Fatalf("pack() length = %d, want %d", got, createMsgSerializedBufSize)
	}

	offset := createMsgPrefixSize + createMsgPaddingAfterCPU
	gotInts := make([]uint32, createMsgIntFieldCount)
	for i := range gotInts {
		gotInts[i] = binary.LittleEndian.Uint32(buf[offset : offset+createMsgIntFieldSize])
		offset += createMsgIntFieldSize
	}

	wantInts := []uint32{opts.VCPUs, opts.MaxVCPUs, opts.CPUWeight, opts.CPUCapacity, opts.MemoryMB, opts.MemoryMB}
	if !reflect.DeepEqual(gotInts, wantInts) {
		t.Fatalf("packed ints = %v, want %v", gotInts, wantInts)
	}

	iomemStart := offset
	for i := 0; i < MaxConfigStrLen; i++ {
		if buf[iomemStart+i] != 0 {
			t.Fatalf("iomem byte %d = %d, want 0", i, buf[iomemStart+i])
		}
	}

	networkStart := iomemStart + MaxConfigStrLen
	if got := string(bytes.TrimRight(buf[networkStart:networkStart+len(opts.Network)], "\x00")); got != opts.Network {
		t.Fatalf("network data = %q, want %q", got, opts.Network)
	}
}

func TestMaxVCPUsOrDefault(t *testing.T) {
	if got := maxVCPUsOrDefault(4); got != 4 {
		t.Fatalf("maxVCPUsOrDefault(4) = %d, want 4", got)
	}
	if got := maxVCPUsOrDefault(0); got != defs.DefaultMaxVCPUs {
		t.Fatalf("maxVCPUsOrDefault(0) = %d, want %d", got, defs.DefaultMaxVCPUs)
	}
}
