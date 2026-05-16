package shim

import (
	"context"
	"errors"
	"testing"
	"time"

	er "micrun/internal/support/errors"

	"github.com/containerd/containerd/api/events"
	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types/task"
	cdruntime "github.com/containerd/containerd/runtime"
)

func TestShimServiceTaskStoreInitializesAndFiltersNilTasks(t *testing.T) {
	service := &shimService{}

	service.SaveTask("demo", &shimContainer{id: "demo"})
	service.SaveTask("plain-nil", nil)
	var taskHandle *shimContainer
	service.SaveTask("typed-nil", taskHandle)

	got, found := service.LookupTask("demo")
	if !found || got.ID() != "demo" {
		t.Fatalf("LookupTask() = (%#v, %t), want demo task", got, found)
	}
	if _, found := service.LookupTask("typed-nil"); found {
		t.Fatal("typed nil task should not be stored")
	}
	if service.hasShimTask("typed-nil") {
		t.Fatal("hasShimTask() found typed nil task")
	}
	if !service.hasShimTasks() {
		t.Fatal("hasShimTasks() = false, want true")
	}

	service.DeleteTask("demo")
	if service.hasShimTasks() {
		t.Fatal("hasShimTasks() = true after deleting only live task")
	}
}

func TestShimServiceRuntimeIdentityDefaultsAreNilSafe(t *testing.T) {
	var service *shimService

	if service.Namespace() != "" {
		t.Fatal("nil service namespace should be empty")
	}
	if service.BackgroundContext() == nil {
		t.Fatal("nil service background context should fall back to context.Background")
	}
	service.MarkKilledByAPI()
}

func TestShimRPCNilServiceReturnsTaskManagerError(t *testing.T) {
	var service *shimService

	_, err := service.State(context.Background(), &taskAPI.StateRequest{ID: "demo"})
	if err == nil {
		t.Fatal("nil service RPC should return an error")
	}
}

func TestRuntimeStatusRejectsTypedNilSandbox(t *testing.T) {
	var sandbox *typedNilSandboxTraits
	service := &shimService{sandbox: sandbox}

	status, err := service.getContainerStatus(context.Background(), "demo")

	if !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("getContainerStatus error = %v, want SandboxNotFound", err)
	}
	if status != task.Status_UNKNOWN {
		t.Fatalf("status = %s, want UNKNOWN", status)
	}
}

func TestMarshalMetricsRejectsTypedNilSandbox(t *testing.T) {
	var sandbox *typedNilSandboxTraits

	metrics, err := marshalMetrics(context.Background(), metricsSource{sandbox: sandbox}, "demo")

	if !errors.Is(err, er.SandboxNotFound) {
		t.Fatalf("marshalMetrics error = %v, want SandboxNotFound", err)
	}
	if metrics != nil {
		t.Fatalf("metrics = %#v, want nil", metrics)
	}
}

func TestReportTaskExitWithoutExitChannelPublishesDirectly(t *testing.T) {
	service := &shimService{events: make(chan shimEvent, 1)}

	service.ReportTaskExit(&shimContainer{id: "demo"}, 7, time.Unix(10, 0))

	select {
	case ev := <-service.events:
		if ev.topic != cdruntime.TaskExitEventTopic {
			t.Fatalf("topic = %q, want %q", ev.topic, cdruntime.TaskExitEventTopic)
		}
		payload, ok := ev.payload.(*events.TaskExit)
		if !ok {
			t.Fatalf("payload type = %T, want *events.TaskExit", ev.payload)
		}
		if payload.ContainerID != "demo" || payload.ID != "demo" || payload.ExitStatus != 7 {
			t.Fatalf("payload = %+v, want demo main-process exit status 7", payload)
		}
	default:
		t.Fatal("exit event was not published")
	}
}

func TestReportTaskExitSkipsTypedNilTask(t *testing.T) {
	service := &shimService{events: make(chan shimEvent, 1)}
	var taskHandle *shimContainer

	service.ReportTaskExit(taskHandle, 7, time.Now())

	select {
	case ev := <-service.events:
		t.Fatalf("unexpected event: %#v", ev)
	default:
	}
}
