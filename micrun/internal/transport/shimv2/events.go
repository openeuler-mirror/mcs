package shim

import (
	"context"
	"time"

	"micrun/internal/support/contextx"
	"micrun/internal/support/lockutil"
	log "micrun/internal/support/logger"

	"github.com/containerd/containerd/api/events"
	cdruntime "github.com/containerd/containerd/runtime"
	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	eventPublishTimeout   = 5 * time.Second
	ttrpcAddrEnv          = "TTRPC_ADDRESS"
	contdShimEnvSchedCore = "SCHED_CORE"
)

// exitEvent represents a container exitEvent event.
type exitEvent struct {
	ts     time.Time
	cid    string
	execid string
	pid    uint32
	status int
}

type shimEvent struct {
	topic   string
	payload proto.Message
}

// eventsForwarder handles forwarding events from the shim to containerd.
type eventsForwarder struct {
	service   *shimService
	context   context.Context
	publisher shimv2.Publisher
}

// newEventsForwarder creates a new events forwarder
func (s *shimService) newEventsForwarder(ctx context.Context, publisher shimv2.Publisher) *eventsForwarder {
	return &eventsForwarder{
		service:   s,
		context:   ctx,
		publisher: publisher,
	}
}

func getTopic(e proto.Message) string {
	log.Debugf("topic event: %v", e)
	switch e.(type) {
	case *events.TaskCreate:
		return cdruntime.TaskCreateEventTopic
	case *events.TaskStart:
		return cdruntime.TaskStartEventTopic
	case *events.TaskOOM:
		return cdruntime.TaskOOMEventTopic
	case *events.TaskExit:
		return cdruntime.TaskExitEventTopic
	case *events.TaskDelete:
		return cdruntime.TaskDeleteEventTopic
	case *events.TaskExecAdded:
		return cdruntime.TaskExecAddedEventTopic
	case *events.TaskExecStarted:
		return cdruntime.TaskExecStartedEventTopic
	case *events.TaskPaused:
		return cdruntime.TaskPausedEventTopic
	case *events.TaskResumed:
		return cdruntime.TaskResumedEventTopic
	case *events.TaskCheckpointed:
		return cdruntime.TaskCheckpointedEventTopic
	default:
		log.Warnf("no topic for event type: %v", e)
	}
	return cdruntime.TaskUnknownTopic
}

// forward listens for events and publishes them to containerd/isulad
func (ef *eventsForwarder) forward() {
	if ef == nil || ef.service == nil || ef.service.events == nil {
		return
	}
	for e := range ef.service.events {
		// Publish the event to containerd
		ctx, cancel := context.WithTimeout(contextx.OrBackground(ef.context), eventPublishTimeout)
		if err := ef.publisher.Publish(ctx, e.topic, e.payload); err != nil {
			log.Errorf("failed to publish event topic=%s: %v", e.topic, err)
		} else {
			log.Debugf("Successfully forwarded event topic=%s", e.topic)
		}
		cancel()
	}
}

// listenAndReportExits listens for exit events on a channel and reports them.
func (s *shimService) listenAndReportExits() {
	if s == nil || s.ec == nil {
		return
	}
	for e := range s.ec {
		s.reportExit(e)
		if e.execid != "" {
			continue
		}

		if e.cid != s.id {
			if s.consumeKilledByAPI() {
				log.Infof("[SHIM] Pod container %s exited via Kill API, keeping sandbox shim running", e.cid)
			} else {
				log.Infof("[SHIM] Pod container %s exited, keeping sandbox shim running", e.cid)
			}
			continue
		}

		// Keep the shim process alive after the main task exit so containerd can
		// issue the follow-up Delete request over the existing ttrpc socket.
		// Shutdown is handled by the runtime-v2 Shutdown RPC once no tasks remain.
		if s.consumeKilledByAPI() {
			log.Infof("[SHIM] Main container %s exited via Kill API, keeping shim running for cleanup", e.cid)
			continue
		}

		log.Infof("[SHIM] Main container %s exited naturally, keeping shim running for cleanup", e.cid)
		continue
	}
}

// reportExit sends a TaskExit event to containerd.
func (s *shimService) reportExit(e exitEvent) {
	if s == nil {
		return
	}
	id := e.execid
	if id == "" {
		id = e.cid
	}
	s.send(&events.TaskExit{
		ContainerID: e.cid,
		ID:          id,
		Pid:         e.pid,
		ExitStatus:  uint32(e.status),
		ExitedAt:    timestamppb.New(e.ts),
	})
}

func (s *shimService) consumeKilledByAPI() bool {
	if s == nil {
		return false
	}
	return lockutil.WithLockValue(&s.Mutex, func() bool {
		wasKilledByAPI := s.killedByAPI
		s.killedByAPI = false
		return wasKilledByAPI
	})
}

// send places an event on the events channel for forwarding.
func (s *shimService) send(ev proto.Message) {
	if s == nil {
		return
	}
	topic := getTopic(ev)
	if topic == cdruntime.TaskUnknownTopic {
		log.Warnf("unknown event type, skipping: %v", ev)
		return
	}
	if s.publishEvent(topic, ev) {
		return
	}
	if s.events == nil {
		return
	}
	s.events <- shimEvent{topic: topic, payload: ev}
}

func (s *shimService) publishEvent(topic string, ev proto.Message) bool {
	if s == nil || s.publisher == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(contextx.OrBackground(s.ctx)), eventPublishTimeout)
	defer cancel()
	if err := s.publisher.Publish(ctx, topic, ev); err != nil {
		log.Errorf("failed to publish event topic=%s directly: %v", topic, err)
		return false
	}
	log.Debugf("Successfully published event topic=%s directly", topic)
	return true
}
