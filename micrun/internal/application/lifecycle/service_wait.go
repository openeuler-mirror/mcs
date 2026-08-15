package lifecycle

import (
	"errors"
	"time"

	"micrun/internal/application/exitstatus"
	er "micrun/internal/support/errors"
	log "micrun/internal/support/logger"
	"micrun/internal/support/panicsafe"
	"micrun/internal/support/validation"
)

type exitWaitResult int

const (
	exitWaitNoSignal exitWaitResult = iota
	// exitWaitCompleted: the task's own exit channel fired (user exit,
	// no-attach immediate completion). The authoritative exit info has been
	// (or is about to be) recorded by the IO pump, so completeExitedTask
	// must not fabricate a status.
	exitWaitCompleted
	// exitWaitGuestExit: the guest domain disappeared (crash, xl destroy).
	// No exit path recorded a status; completeExitedTask fabricates a
	// non-zero code so the crash is not reported as a clean exit.
	exitWaitGuestExit
)

// exitWaitKillTimeout bounds how long handleWaitCanceled waits for an exit
// signal after issuing an internal kill. If the guest is unreachable and
// the container never transitions to Stopped/Down, this prevents the
// goroutine from blocking forever.
const exitWaitKillTimeout = 30 * time.Second

// guestExitRetryDelay is the backoff between WaitContainerExit retries when
// the guest RPC returns a transient error (timeout, micad restart). Without
// retry, a single transient failure permanently disables crash detection.
const guestExitRetryDelay = 5 * time.Second

func (s *Service) waitForExit(tc *taskContext) int32 {
	return s.waitForExitWithPolicy(tc, resolveWaitPolicy(tc.Task))
}

// waitForExitWithPolicy is the core exit-wait loop with an explicit policy.
// Split from waitForExit so WatchExit (recovery path) can disable auto-close.
func (s *Service) waitForExitWithPolicy(tc *taskContext, policy waitPolicy) int32 {
	result := s.waitForExitSignal(tc, policy)
	if result == exitWaitNoSignal {
		return 0
	}
	return s.completeExitedTask(tc, result)
}

func (s *Service) completeExitedTask(tc *taskContext, result exitWaitResult) int32 {
	// Snapshot status/time/had in a single lock acquisition so a concurrent
	// IO pump that records a clean exit (SetExitInfo then IOExit) cannot be
	// interleaved between two separate samples.
	exitStatus, exitedAt, hadExitInfo := s.taskExitInfo(tc)
	// Only a guest-driven exit fabricates a non-zero status: the task never
	// exited through a normal path, so a default 0 would look like a clean
	// exit (contradicting internalKill's Interrupt policy). An exitCh-driven
	// completion already carries the authoritative status.
	if result == exitWaitGuestExit && exitStatus == exitstatus.Success && !hadExitInfo {
		exitStatus = exitstatus.Interrupt()
	}
	// Publish the exit BEFORE stopping the sandbox: stopTaskAfterExit runs a
	// slow guest RPC, and doing it first could delay TaskExit behind a
	// concurrent Delete's TaskDelete event (containerd consumers keyed on
	// TaskExit would miss the exit). Exit reporting must not depend on the
	// guest stop succeeding.
	markTaskStopped(tc.Runtime, tc.Task, exitStatus, exitedAt)
	// Re-read the authoritative exit info after markTaskStopped: a Kill
	// pre-write (or concurrent stopFromIOEvent) may have kept a different
	// code than our snapshot/fabricated value. ReportTaskExit and the return
	// code must match what is actually stored on the task.
	finalStatus, finalAt, _ := s.taskExitInfo(tc)
	// Close the task's exit channel so any in-flight Wait RPC (snapshotted
	// while the task was still RUNNING) unblocks. exitIOch is otherwise only
	// closed by attach IO events, Kill, or Delete — none of which fire for a
	// guest-driven exit, leaving the Wait RPC blocked forever.
	signalTaskIOExit(tc.Task)
	// Stop the task's IO session before publishing the exit: with the guest
	// gone the copier has no live peer, and containerd's ContainerIO.Wait
	// (inside task.Delete, driven by CRI's handleContainerExit) only
	// completes once the FIFO write ends close. Leaving the session running
	// wedges every consumer behind that wait — kubelet StopContainer and
	// StopPodSandbox time out and the pod sticks in Terminating (observed
	// with K3s pod deletion: domain destroyed, TaskExit published, yet the
	// task record was never removed). Stop is idempotent and bounded by
	// copierStopTimeout, and covers the kill/crash/natural exits that have
	// no attach client left to close the streams.
	// Report the exit before the (bounded-blocking) IO stop: the Delete RPC
	// path emits its TaskDeleted event concurrently, and containerd consumers
	// expect TaskExit to be observable first.
	tc.Runtime.ReportTaskExit(tc.Task, int(finalStatus), finalAt)
	stopTaskIOSession(tc.Task)
	s.stopTaskAfterExit(tc)
	return int32(finalStatus)
}
func (s *Service) taskExitInfo(tc *taskContext) (uint32, time.Time, bool) {
	clockNow := s.clockNow()
	return snapshotTaskExitInfo(tc.Runtime, tc.Task, clockNow)
}

// guestExitSignal returns a channel that fires when the RTOS guest domain
// disappears (crashed, xl destroy, micad restart). Without this, a task whose
// guest dies spontaneously would stay RUNNING forever and its Wait RPC would
// block indefinitely — exitIOch is only closed by user-typed exit, Kill, or
// Delete. The goroutine is bounded by the sandbox WaitContainerExit call,
// which itself cancels when the sandbox context ends.
func guestExitSignal(tc *taskContext) <-chan struct{} {
	ch := make(chan struct{})
	panicsafe.Go("guest exit signal watcher", func() {
		sandbox := snapshotRuntimeSandbox(tc.Runtime)
		if validation.IsNil(sandbox) {
			return
		}
		// WaitContainerExit blocks until the container transitions to
		// Stopped/Down (guest gone) or the context is canceled. When the
		// guest is already gone, checkStateWithContext marks it Down and
		// WaitContainerExit returns er.ContainerDown — that error IS the
		// exit signal, so treat it as guest-exited. Only a context
		// cancellation (shim shutdown) is not an exit.
		//
		// Transient RPC errors (micad restart, timeout) must NOT permanently
		// abandon the watcher — retry with backoff so a later guest crash is
		// still detected. Without this, a single transient failure leaves the
		// task's exit channel open forever and Wait RPCs hang indefinitely.
		for {
			_, err := sandbox.WaitContainerExit(tc.Context, tc.Task.ID())
			if err == nil || errors.Is(err, er.ContainerDown) {
				close(ch)
				return
			}
			// The container was deleted from the sandbox (Delete raced the
			// watcher's retry sleep): there is nothing left to watch, which
			// semantically IS an exit. close(ch) so waitForExitSignal reports
			// it — returning silently leaves the task RUNNING and Wait
			// hanging when the removal came from a sandbox-level Kill (only
			// the task-level Delete path closes the task's exit channel
			// itself; killSandboxTask does not touch workload task handles).
			if errors.Is(err, er.ContainerNotFound) {
				log.Debugf("guestExitSignal: container %s no longer exists, reporting exit", tc.Task.ID())
				close(ch)
				return
			}
			log.Warnf("guestExitSignal: WaitContainerExit for %s returned transient error: %v; retrying in %v", tc.Task.ID(), err, guestExitRetryDelay)
			select {
			case <-tc.Context.Done():
				return
			case <-time.After(guestExitRetryDelay):
			}
		}
	})
	return ch
}

func (s *Service) waitForExitSignal(tc *taskContext, policy waitPolicy) exitWaitResult {
	exitCh := tc.Task.ExitChan()
	if exitCh == nil {
		log.Errorf("task %s has no exit signal channel", tc.Task.ID())
		return exitWaitNoSignal
	}

	guestExit := guestExitSignal(tc)

	// Optional capability: attach-state transition notifications. When the
	// task provides them, the grace timer is driven by the actual detach
	// moment instead of sampling IsAttached at timer expiry (the old
	// sampling made the effective grace depend on where the disconnect fell
	// inside the timer cycle — worst case a long-lived attach session ending
	// 1ms before expiry gave the client ~0s instead of the promised N).
	type attachNotifier interface{ AttachChanges() <-chan struct{} }
	var attachChanges <-chan struct{}
	if policy.autoClose {
		if n, ok := tc.Task.(attachNotifier); ok {
			attachChanges = n.AttachChanges()
		}
	}

	var timer *time.Timer
	if policy.autoClose {
		if tc.Task.IsAttached() {
			// Already attached at watcher start: the grace window only
			// begins when the client actually disconnects.
			timer = time.NewTimer(time.Duration(1<<62 - 1))
		} else {
			timer = time.NewTimer(policy.timeout)
		}
		defer timer.Stop()
	}

	if !policy.autoClose || timer == nil {
		select {
		case <-exitCh:
			log.Debugf("received exit signal for container %s.", tc.Task.ID())
			return exitWaitCompleted
		case <-guestExit:
			log.Infof("container %s guest exited (domain gone).", tc.Task.ID())
			return exitWaitGuestExit
		case <-tc.Context.Done():
			return s.handleWaitCanceled(tc, exitCh)
		}
	}

	// The auto-close timer must not kill an interactive session while the
	// attach client is still connected: the annotation semantics are "close
	// N seconds after the IO session disconnects", not "close N seconds
	// after start". Reset the timer while a client is attached.
	for {
		select {
		case <-exitCh:
			log.Debugf("The container %s IO streams closed.", tc.Task.ID())
			return exitWaitCompleted
		case <-guestExit:
			log.Infof("container %s guest exited (domain gone).", tc.Task.ID())
			return exitWaitGuestExit
		case <-tc.Context.Done():
			return s.handleWaitCanceled(tc, exitCh)
		case <-attachChanges:
			if tc.Task.IsAttached() {
				log.Debugf("[TIMEOUT] %s reattached; auto-close grace suspended", tc.Task.ID())
				timer.Stop()
			} else {
				log.Debugf("[TIMEOUT] %s detached; auto-close grace started (%v)", tc.Task.ID(), policy.timeout)
				// Drain per the time.Timer docs: if the timer already fired
				// and its value sits unread in the channel, a bare Reset
				// would let the next loop iteration consume the stale tick
				// and kill the freshly detached session with zero grace —
				// the exact collapse this rework exists to eliminate.
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(policy.timeout)
			}
		case <-timer.C:
			// Re-read immediately before kill: a client may have reattached
			// after the timer fired. internalKill itself is slow and unlocked;
			// if a reconnect races the kill, the next Start/EnsureAttach path
			// is the recovery, not a second IsAttached sample here.
			if tc.Task.IsAttached() {
				log.Debugf("[TIMEOUT] %s still attached, resetting auto-close timer to %v", tc.Task.ID(), policy.timeout)
				timer.Reset(policy.timeout)
				continue
			}
			log.Infof("[TIMEOUT] Auto-closing %s after %v timeout.", tc.Task.ID(), policy.timeout)
			s.internalKill(tc, "auto-close-timeout")
			return exitWaitCompleted
		}
	}
}

func (s *Service) handleWaitCanceled(tc *taskContext, exitCh <-chan struct{}) exitWaitResult {
	log.Infof("waitForExit canceled for %s: %v", tc.Task.ID(), tc.Context.Err())
	s.internalKill(tc, "wait-canceled")
	// Wait for the exit signal, but bound the wait so that a kill that fails
	// to drive the container into Stopped/Down (e.g. unreachable guest) does
	// not block this goroutine forever.
	select {
	case <-exitCh:
	case <-time.After(exitWaitKillTimeout):
		log.Warnf("waitForExit for %s: exit signal not received within 30s after kill, giving up", tc.Task.ID())
	}
	return exitWaitCompleted
}

func (s *Service) stopTaskAfterExit(tc *taskContext) {
	sandbox := snapshotRuntimeSandbox(tc.Runtime)
	stopLifecycleTask(tc.Context, tc.Runtime, sandbox, tc.Task, taskStopOptions{reason: "after exit"})
}
