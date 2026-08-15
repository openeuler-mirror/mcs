package shim

import (
	"testing"
	"time"

	"github.com/containerd/containerd/api/events"
	cdruntime "github.com/containerd/containerd/runtime"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestSendQueuesEventWithTopic(t *testing.T) {
	service := &shimService{events: make(chan shimEvent, 1)}

	service.send(&events.TaskStart{ContainerID: "container1"})

	select {
	case ev := <-service.events:
		if ev.topic != cdruntime.TaskStartEventTopic {
			t.Fatalf("topic = %q, want %q", ev.topic, cdruntime.TaskStartEventTopic)
		}
		if _, ok := ev.payload.(*events.TaskStart); !ok {
			t.Fatalf("payload type = %T, want *events.TaskStart", ev.payload)
		}
	default:
		t.Fatal("event was not queued")
	}
}

func TestSendSkipsUnknownEvent(t *testing.T) {
	service := &shimService{events: make(chan shimEvent, 1)}

	service.send(&timestamppb.Timestamp{})

	select {
	case ev := <-service.events:
		t.Fatalf("unexpected event queued: %#v", ev)
	default:
	}
}

func TestEventHelpersAreNilSafe(t *testing.T) {
	var service *shimService

	service.send(&events.TaskStart{ContainerID: "container1"})
	service.reportExit(exitEvent{cid: "container1", ts: time.Now()})
	service.listenAndReportExits()
	if service.consumeKilledByAPI() {
		t.Fatal("nil service should not report killed-by-API state")
	}
}

func TestListenAndReportExitsKeepsShimForNaturalMainExit(t *testing.T) {
	shutdownCalled := make(chan struct{}, 1)
	exited := make(chan struct{}, 1)
	service := &shimService{
		id:     "container1",
		events: make(chan shimEvent, 1),
		ec:     make(chan exitEvent, 1),
		ss: func() {
			shutdownCalled <- struct{}{}
		},
		shutdown: shutdownEffects{
			readAddress: func(string) (string, error) { return "", nil },
			exit: func(code int) {
				if code != 0 {
					t.Errorf("exit code = %d, want 0", code)
				}
				exited <- struct{}{}
			},
		},
	}

	go service.listenAndReportExits()
	service.ec <- exitEvent{cid: "container1", ts: time.Now()}

	select {
	case ev := <-service.events:
		payload, ok := ev.payload.(*events.TaskExit)
		if !ok {
			t.Fatalf("event type = %T, want *events.TaskExit", ev.payload)
		}
		if payload.ContainerID != "container1" || payload.ID != "container1" {
			t.Fatalf("payload = %+v, want container1 main-process exit", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("exit event was not published")
	}

	select {
	case <-exited:
		t.Fatal("shutdown exit effect was called before Delete/Shutdown")
	case <-shutdownCalled:
		t.Fatal("shutdown callback was called before Delete/Shutdown")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestListenAndReportExitsKeepsSandboxShimForPodContainerExit(t *testing.T) {
	shutdownCalled := make(chan struct{}, 1)
	service := &shimService{
		id:     "sandbox1",
		events: make(chan shimEvent, 1),
		ec:     make(chan exitEvent, 1),
		ss: func() {
			shutdownCalled <- struct{}{}
		},
		shutdown: shutdownEffects{
			exit: func(code int) {
				t.Errorf("shutdown exit called with code %d", code)
			},
		},
	}
	service.killedByAPI.Store(true)

	go service.listenAndReportExits()
	service.ec <- exitEvent{cid: "app1", ts: time.Now()}

	select {
	case ev := <-service.events:
		payload, ok := ev.payload.(*events.TaskExit)
		if !ok {
			t.Fatalf("event type = %T, want *events.TaskExit", ev.payload)
		}
		if payload.ContainerID != "app1" || payload.ID != "app1" {
			t.Fatalf("payload = %+v, want app1 main-process exit", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("exit event was not published")
	}

	select {
	case <-shutdownCalled:
		t.Fatal("sandbox shim was shut down for pod container exit")
	case <-time.After(50 * time.Millisecond):
	}
	if service.consumeKilledByAPI() {
		t.Fatal("pod container exit did not consume killed-by-API marker")
	}
}
