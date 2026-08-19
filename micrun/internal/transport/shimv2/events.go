package shim

import (
	"context"
	"time"

	"micrun/internal/support/contextx"
	log "micrun/internal/support/logger"
	"micrun/internal/support/panicsafe"

	"github.com/containerd/containerd/api/events"
	cdruntime "github.com/containerd/containerd/runtime"
	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	eventPublishTimeout = 5 * time.Second
	// eventChannelBlockTimeout bounds how long send() blocks on a full
	// events channel for critical (exit/delete) events. Deliberately much
	// shorter than eventPublishTimeout so a stalled forwarder cannot hold
	// RPC handlers for ~10s (5s direct publish + 5s queue).
	eventChannelBlockTimeout = 1 * time.Second
	ttrpcAddrEnv             = "TTRPC_ADDRESS"
	contdShimEnvSchedCore    = "SCHED_CORE"
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
		// Publish the event in a per-iteration helper so cancel runs via
		// defer at the end of EACH publish (defer in the loop body itself
		// would accumulate until forward() returns). defer also guarantees
		// the timeout context is released if Publish panics — an un-caught
		// panic here would otherwise leak the timer goroutine and kill the
		// sole forwarder, stalling every subsequent event.
		ef.publishOne(e)
	}
}

// publishOne publishes a single event under a bounded timeout context. The
// per-event panic guard keeps the sole forwarder loop alive when a single
// Publish panics: losing one event is recoverable, losing the forwarder
// silently drops every subsequent TaskExit/TaskDelete.
func (ef *eventsForwarder) publishOne(e shimEvent) {
	defer panicsafe.Recover("event publish")
	ctx, cancel := context.WithTimeout(contextx.OrBackground(ef.context), eventPublishTimeout)
	defer cancel()
	if err := ef.publisher.Publish(ctx, e.topic, e.payload); err != nil {
		log.Errorf("failed to publish event topic=%s: %v", e.topic, err)
		return
	}
	log.Debugf("Successfully forwarded event topic=%s", e.topic)
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
	return s.killedByAPI.Swap(false)
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
	if s.events == nil {
		return
	}
	// Events are published by the single forwarder goroutine: RPC handlers
	// must never block on network I/O (a slow containerd publisher would
	// stall Delete/Start RPCs well past their client timeouts). Exit/Delete
	// events are part of containerd's Wait/Delete contract, so they block
	// (bounded) in the hope the forwarder drains the channel, instead of
	// dropping immediately; lower-value events are dropped when the channel
	// is full.
	if isCriticalEvent(topic) {
		// Use a reusable timer (stopped on the success path) rather than
		// time.After: time.After's timer is not collected until it fires, so
		// under bursty critical-event traffic each enqueued event would pin a
		// ~1s timer until expiry.
		timer := time.NewTimer(eventChannelBlockTimeout)
		select {
		case s.events <- shimEvent{topic: topic, payload: ev}:
			if !timer.Stop() {
				<-timer.C
			}
		case <-timer.C:
			log.Errorf("event channel full, dropping critical event after %v (topic=%s)", eventChannelBlockTimeout, topic)
		}
		return
	}
	select {
	case s.events <- shimEvent{topic: topic, payload: ev}:
	default:
		log.Warnf("event channel full, dropping event (topic=%s)", topic)
	}
}

// isCriticalEvent reports whether a dropped event would break a containerd
// client contract (Wait RPC / Delete RPC completion).
func isCriticalEvent(topic string) bool {
	return topic == cdruntime.TaskExitEventTopic || topic == cdruntime.TaskDeleteEventTopic
}
