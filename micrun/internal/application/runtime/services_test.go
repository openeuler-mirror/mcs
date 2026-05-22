package runtime

import (
	"errors"
	"strings"
	"testing"
	"time"

	attachapp "micrun/internal/application/attach"
	lifecycleapp "micrun/internal/application/lifecycle"
	taskapp "micrun/internal/application/task"
)

func TestNewServicesBuildsTaskAndRecoveryServices(t *testing.T) {
	services, err := NewServicesChecked(Options{})
	if err != nil {
		t.Fatalf("NewServicesChecked returned error: %v", err)
	}

	if services.Attach() == nil {
		t.Fatal("Attach service is nil")
	}
	if services.Lifecycle() == nil {
		t.Fatal("Lifecycle service is nil")
	}
	if services.Task() == nil {
		t.Fatal("Task service is nil")
	}
	if services.Recovery() == nil {
		t.Fatal("Recovery service is nil")
	}
}

func TestNewServicesUsesSharedAttachService(t *testing.T) {
	services, err := NewServicesChecked(Options{})
	if err != nil {
		t.Fatalf("NewServicesChecked returned error: %v", err)
	}

	if services.Lifecycle().AttachService() != services.Attach() {
		t.Fatal("lifecycle service does not share runtime attach service")
	}
	if services.Task().AttachService() != services.Attach() {
		t.Fatal("task service does not share runtime attach service")
	}
	if services.Task().LifecycleService() != services.Lifecycle() {
		t.Fatal("task service does not share runtime lifecycle service")
	}
}

func TestNewServicesCheckedReturnsValidatedGraph(t *testing.T) {
	services, err := NewServicesChecked(Options{})
	if err != nil {
		t.Fatalf("NewServicesChecked returned error: %v", err)
	}
	if err := services.Validate(); err != nil {
		t.Fatalf("validated services should remain valid: %v", err)
	}
}

func TestValidateRejectsTypedNilServices(t *testing.T) {
	err := (Services{}).Validate()
	if err == nil {
		t.Fatal("expected empty service graph to fail validation")
	}
	if !errors.Is(err, ErrServicesIncomplete) {
		t.Fatalf("validation error = %v, want incomplete sentinel", err)
	}
}

func TestValidateRejectsTypedNilTaskAndRecovery(t *testing.T) {
	services, err := NewServicesChecked(Options{})
	if err != nil {
		t.Fatalf("NewServicesChecked returned error: %v", err)
	}
	services.task = nil
	services.recovery = nil

	validateErr := services.Validate()
	if validateErr == nil {
		t.Fatal("expected typed nil task and recovery services to fail validation")
	}
	if !errors.Is(validateErr, ErrServicesIncomplete) {
		t.Fatalf("validation error = %v, want incomplete sentinel", validateErr)
	}
}

func TestValidateUsesRequiredServiceLabels(t *testing.T) {
	err := (Services{}).Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "attach service") {
		t.Fatalf("validation error = %v, want attach service reason", err)
	}
}

func TestValidateRejectsMismatchedRuntimeServiceGraph(t *testing.T) {
	services, err := NewServicesChecked(Options{})
	if err != nil {
		t.Fatalf("NewServicesChecked returned error: %v", err)
	}
	otherAttach, err := attachapp.NewServiceChecked(nil)
	if err != nil {
		t.Fatalf("attach service setup failed: %v", err)
	}
	otherLifecycle, err := lifecycleapp.NewServiceChecked(otherAttach)
	if err != nil {
		t.Fatalf("lifecycle service setup failed: %v", err)
	}
	otherTask, err := taskapp.NewServiceChecked(otherAttach, otherLifecycle)
	if err != nil {
		t.Fatalf("task service setup failed: %v", err)
	}
	services.task = otherTask

	if err := services.Validate(); err == nil {
		t.Fatal("expected inconsistent services to fail validation")
	} else if !errors.Is(err, ErrServicesInconsistent) {
		t.Fatalf("validation error = %v, want inconsistent sentinel", err)
	}
}

func TestValidateRejectsMismatchedLifecycleAttachService(t *testing.T) {
	services, err := NewServicesChecked(Options{})
	if err != nil {
		t.Fatalf("NewServicesChecked returned error: %v", err)
	}
	otherAttach, err := attachapp.NewServiceChecked(nil)
	if err != nil {
		t.Fatalf("attach service setup failed: %v", err)
	}
	lifecycle, err := lifecycleapp.NewServiceChecked(otherAttach)
	if err != nil {
		t.Fatalf("lifecycle service setup failed: %v", err)
	}
	services.lifecycle = lifecycle

	err = services.Validate()
	if err == nil {
		t.Fatal("expected mismatched lifecycle attach service to fail")
	}
	if !errors.Is(err, ErrServicesInconsistent) {
		t.Fatalf("validation error = %v, want inconsistent sentinel", err)
	}
	if !errors.Is(err, ErrLifecycleAttachServiceMismatch) {
		t.Fatalf("validation error = %v, want lifecycle attach mismatch sentinel", err)
	}
}

func TestValidateRejectsMismatchedTaskAttachService(t *testing.T) {
	services, err := NewServicesChecked(Options{})
	if err != nil {
		t.Fatalf("NewServicesChecked returned error: %v", err)
	}
	otherAttach, err := attachapp.NewServiceChecked(nil)
	if err != nil {
		t.Fatalf("attach service setup failed: %v", err)
	}
	lifecycle, err := lifecycleapp.NewServiceChecked(otherAttach)
	if err != nil {
		t.Fatalf("lifecycle service setup failed: %v", err)
	}
	otherTask, err := taskapp.NewServiceChecked(otherAttach, lifecycle)
	if err != nil {
		t.Fatalf("task service setup failed: %v", err)
	}
	services.task = otherTask

	err = services.Validate()
	if err == nil {
		t.Fatal("expected mismatched task attach service to fail")
	}
	if !errors.Is(err, ErrServicesInconsistent) {
		t.Fatalf("validation error = %v, want inconsistent sentinel", err)
	}
	if !errors.Is(err, ErrTaskAttachServiceMismatch) {
		t.Fatalf("validation error = %v, want task attach mismatch sentinel", err)
	}
}

func TestValidateRejectsMismatchedTaskLifecycleService(t *testing.T) {
	services, err := NewServicesChecked(Options{})
	if err != nil {
		t.Fatalf("NewServicesChecked returned error: %v", err)
	}
	otherLifecycle, err := lifecycleapp.NewServiceChecked(services.Attach())
	if err != nil {
		t.Fatalf("other lifecycle service setup failed: %v", err)
	}
	otherTask, err := taskapp.NewServiceChecked(services.Attach(), otherLifecycle)
	if err != nil {
		t.Fatalf("task service setup failed: %v", err)
	}
	services.task = otherTask

	err = services.Validate()
	if err == nil {
		t.Fatal("expected mismatched task lifecycle service to fail")
	}
	if !errors.Is(err, ErrServicesInconsistent) {
		t.Fatalf("validation error = %v, want inconsistent sentinel", err)
	}
	if !errors.Is(err, ErrTaskLifecycleServiceMismatch) {
		t.Fatalf("validation error = %v, want task lifecycle mismatch sentinel", err)
	}
}

func TestNewServicesCheckedAcceptsSharedClockOption(t *testing.T) {
	now := time.Date(2026, 5, 1, 1, 2, 3, 0, time.UTC)

	services, err := NewServicesChecked(Options{
		Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewServicesChecked returned error: %v", err)
	}
	if services.Attach() == nil || services.Lifecycle() == nil || services.Task() == nil {
		t.Fatal("expected shared clock configuration to build complete services")
	}
}
