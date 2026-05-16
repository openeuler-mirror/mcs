package task

import (
	"micrun/internal/ports"

	attachapp "micrun/internal/application/attach"
	lifecycleapp "micrun/internal/application/lifecycle"
)

func NewService(ioFactory ports.IOSessionFactory, opts ...Option) *Service {
	attach, err := attachapp.NewServiceChecked(ioFactory)
	if err != nil {
		panic(err)
	}
	lifecycle, err := lifecycleapp.NewServiceChecked(attach)
	if err != nil {
		panic(err)
	}
	service, err := NewServiceChecked(attach, lifecycle, opts...)
	if err != nil {
		panic(err)
	}
	return service
}
