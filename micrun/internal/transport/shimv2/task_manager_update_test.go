package shim

import (
	"testing"

	apptask "micrun/internal/application/task"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	ptypes "github.com/containerd/containerd/protobuf/types"
	"github.com/containerd/typeurl/v2"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// marshalResources wraps LinuxResources into the Any proto the task API carries.
func marshalResources(t *testing.T, lr *specs.LinuxResources) *ptypes.Any {
	t.Helper()
	any, err := typeurl.MarshalAny(lr)
	if err != nil {
		t.Fatalf("marshal LinuxResources: %v", err)
	}
	return &ptypes.Any{
		TypeUrl: any.GetTypeUrl(),
		Value:   any.GetValue(),
	}
}

func TestUpdateInputFromTransportTranslatesLinuxResources(t *testing.T) {
	memLimit := int64(64 * 1024 * 1024)
	quota := int64(100000)
	period := uint64(100000)

	cases := []struct {
		name string
		req  *taskAPI.UpdateTaskRequest
		want apptask.UpdateInput
	}{
		{
			name: "memory only",
			req: &taskAPI.UpdateTaskRequest{
				ID: "c1",
				Resources: marshalResources(t, &specs.LinuxResources{
					Memory: &specs.LinuxMemory{Limit: &memLimit},
				}),
			},
			want: apptask.UpdateInput{
				ID: "c1",
				Resources: specs.LinuxResources{
					Memory: &specs.LinuxMemory{Limit: &memLimit},
				},
			},
		},
		{
			name: "cpu quota and period",
			req: &taskAPI.UpdateTaskRequest{
				ID: "c2",
				Resources: marshalResources(t, &specs.LinuxResources{
					CPU: &specs.LinuxCPU{Quota: &quota, Period: &period},
				}),
			},
			want: apptask.UpdateInput{
				ID: "c2",
				Resources: specs.LinuxResources{
					CPU: &specs.LinuxCPU{Quota: &quota, Period: &period},
				},
			},
		},
		{
			name: "cpuset only",
			req: &taskAPI.UpdateTaskRequest{
				ID: "c3",
				Resources: marshalResources(t, &specs.LinuxResources{
					CPU: &specs.LinuxCPU{Cpus: "0-1"},
				}),
			},
			want: apptask.UpdateInput{
				ID: "c3",
				Resources: specs.LinuxResources{
					CPU: &specs.LinuxCPU{Cpus: "0-1"},
				},
			},
		},
		{
			name: "all fields combined",
			req: &taskAPI.UpdateTaskRequest{
				ID: "c4",
				Resources: marshalResources(t, &specs.LinuxResources{
					Memory: &specs.LinuxMemory{Limit: &memLimit},
					CPU: &specs.LinuxCPU{
						Quota:  &quota,
						Period: &period,
						Cpus:   "2-3",
					},
				}),
			},
			want: apptask.UpdateInput{
				ID: "c4",
				Resources: specs.LinuxResources{
					Memory: &specs.LinuxMemory{Limit: &memLimit},
					CPU: &specs.LinuxCPU{
						Quota:  &quota,
						Period: &period,
						Cpus:   "2-3",
					},
				},
			},
		},
		{
			name: "nil resources yields empty input",
			req: &taskAPI.UpdateTaskRequest{
				ID:        "c5",
				Resources: nil,
			},
			want: apptask.UpdateInput{
				ID:        "c5",
				Resources: specs.LinuxResources{},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := updateInputFromTransport(tc.req)
			if err != nil {
				t.Fatalf("updateInputFromTransport returned error: %v", err)
			}
			if got.ID != tc.want.ID {
				t.Errorf("ID = %q, want %q", got.ID, tc.want.ID)
			}
			wantMem := int64(0)
			if tc.want.Resources.Memory != nil && tc.want.Resources.Memory.Limit != nil {
				wantMem = *tc.want.Resources.Memory.Limit
			}
			gotMem := int64(0)
			if got.Resources.Memory != nil && got.Resources.Memory.Limit != nil {
				gotMem = *got.Resources.Memory.Limit
			}
			if gotMem != wantMem {
				t.Errorf("memory limit = %d, want %d", gotMem, wantMem)
			}
			wantCpus := ""
			if tc.want.Resources.CPU != nil {
				wantCpus = tc.want.Resources.CPU.Cpus
			}
			gotCpus := ""
			if got.Resources.CPU != nil {
				gotCpus = got.Resources.CPU.Cpus
			}
			if gotCpus != wantCpus {
				t.Errorf("cpuset = %q, want %q", gotCpus, wantCpus)
			}
		})
	}
}

func TestLinuxResourcesFromUpdateRequestRejectsMalformedPayload(t *testing.T) {
	// Marshal a non-LinuxResources type to exercise the type assertion.
	bad := &ptypes.Any{TypeUrl: "type.googleapis.com/specs.LinuxCPU", Value: []byte("not-linuxresources")}
	req := &taskAPI.UpdateTaskRequest{ID: "c-bad", Resources: bad}
	if _, err := linuxResourcesFromUpdateRequest(req); err == nil {
		t.Fatal("expected error for non-LinuxResources payload, got nil")
	}
}
