package runtime

import (
	"errors"

	attachapp "micrun/internal/application/attach"
	lifecycleapp "micrun/internal/application/lifecycle"
	recoveryapp "micrun/internal/application/recovery"
	taskapp "micrun/internal/application/task"
	"micrun/internal/ports"
	"micrun/internal/support/timex"
	"micrun/internal/support/validation"
)

type Services struct {
	attach    *attachapp.Service
	lifecycle *lifecycleapp.Service
	task      *taskapp.Service
	recovery  *recoveryapp.Service
}

var (
	ErrServicesIncomplete             = errors.New("runtime services are incomplete")
	ErrServicesInconsistent           = errors.New("runtime services are inconsistent")
	ErrLifecycleAttachServiceMismatch = errors.New("lifecycle attach service does not match runtime attach service")
	ErrTaskAttachServiceMismatch      = errors.New("task attach service does not match runtime attach service")
	ErrTaskLifecycleServiceMismatch   = errors.New("task lifecycle service does not match runtime lifecycle service")
)

type Options struct {
	IOFactory ports.IOSessionFactory
	Clock     timex.Clock
}

func NewServicesChecked(options Options) (Services, error) {
	attach, err := attachapp.NewServiceChecked(options.IOFactory, attachapp.WithClock(options.Clock))
	if err != nil {
		return Services{}, err
	}
	lifecycle, err := lifecycleapp.NewServiceChecked(attach, lifecycleapp.WithClock(options.Clock))
	if err != nil {
		return Services{}, err
	}
	task, err := taskapp.NewServiceChecked(attach, lifecycle, taskapp.WithClock(options.Clock))
	if err != nil {
		return Services{}, err
	}
	services := Services{
		attach:    attach,
		lifecycle: lifecycle,
		task:      task,
		recovery:  recoveryapp.NewService(),
	}
	if err := services.Validate(); err != nil {
		return Services{}, err
	}
	return services, nil
}

func (s Services) Validate() error {
	if err := validation.RequireAll("runtime services are incomplete",
		validation.Required("attach service", s.attach),
		validation.Required("lifecycle service", s.lifecycle),
		validation.Required("task service", s.task),
		validation.Required("recovery service", s.recovery),
	); err != nil {
		return errors.Join(ErrServicesIncomplete, err)
	}
	if s.lifecycle.AttachService() != s.attach {
		return errors.Join(ErrServicesInconsistent, ErrLifecycleAttachServiceMismatch)
	}
	if s.task.AttachService() != s.attach {
		return errors.Join(ErrServicesInconsistent, ErrTaskAttachServiceMismatch)
	}
	if s.task.LifecycleService() != s.lifecycle {
		return errors.Join(ErrServicesInconsistent, ErrTaskLifecycleServiceMismatch)
	}
	return nil
}

func (s Services) Task() *taskapp.Service {
	return s.task
}

func (s Services) Attach() *attachapp.Service {
	return s.attach
}

func (s Services) Lifecycle() *lifecycleapp.Service {
	return s.lifecycle
}

func (s Services) Recovery() *recoveryapp.Service {
	return s.recovery
}
