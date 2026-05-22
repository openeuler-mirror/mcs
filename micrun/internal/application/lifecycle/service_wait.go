package lifecycle

import (
	"time"

	log "micrun/internal/support/logger"
)

type exitWaitResult int

const (
	exitWaitNoSignal exitWaitResult = iota
	exitWaitCompleted
)

func (s *Service) waitForExit(tc *taskContext) int32 {
	policy := resolveWaitPolicy(tc.Task)
	if result := s.waitForExitSignal(tc, policy); result != exitWaitCompleted {
		return 0
	}
	return s.completeExitedTask(tc)
}

func (s *Service) completeExitedTask(tc *taskContext) int32 {
	exitStatus, exitedAt := s.taskExitInfo(tc)
	s.stopTaskAfterExit(tc)

	markTaskStopped(tc.Runtime, tc.Task, exitStatus, exitedAt)
	tc.Runtime.ReportTaskExit(tc.Task, int(exitStatus), exitedAt)
	return int32(exitStatus)
}
func (s *Service) taskExitInfo(tc *taskContext) (uint32, time.Time) {
	clockNow := s.clockNow()
	return snapshotTaskExitInfo(tc.Runtime, tc.Task, clockNow)
}

func (s *Service) waitForExitSignal(tc *taskContext, policy waitPolicy) exitWaitResult {
	exitCh := tc.Task.ExitChan()
	if exitCh == nil {
		log.Errorf("task %s has no exit signal channel", tc.Task.ID())
		return exitWaitNoSignal
	}

	var timer *time.Timer
	if policy.autoClose {
		timer = time.NewTimer(policy.timeout)
		defer timer.Stop()
	}

	if !policy.autoClose || timer == nil {
		select {
		case <-exitCh:
			log.Debugf("received exit signal for container %s.", tc.Task.ID())
			return exitWaitCompleted
		case <-tc.Context.Done():
			return s.handleWaitCanceled(tc, exitCh)
		}
	}

	select {
	case <-exitCh:
		log.Debugf("The container %s IO streams closed.", tc.Task.ID())
		return exitWaitCompleted
	case <-tc.Context.Done():
		return s.handleWaitCanceled(tc, exitCh)
	case <-timer.C:
		log.Infof("[TIMEOUT] Auto-closing %s after %v timeout.", tc.Task.ID(), policy.timeout)
		s.internalKill(tc, "auto-close-timeout")
		return exitWaitCompleted
	}
}

func (s *Service) handleWaitCanceled(tc *taskContext, exitCh <-chan struct{}) exitWaitResult {
	log.Infof("waitForExit canceled for %s: %v", tc.Task.ID(), tc.Context.Err())
	s.internalKill(tc, "wait-canceled")
	<-exitCh
	return exitWaitCompleted
}

func (s *Service) stopTaskAfterExit(tc *taskContext) {
	sandbox := snapshotRuntimeSandbox(tc.Runtime)
	stopLifecycleTask(tc.Context, tc.Runtime, sandbox, tc.Task, taskStopOptions{reason: "after exit"})
}
