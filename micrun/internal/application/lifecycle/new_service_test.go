package lifecycle

import (
	attachapp "micrun/internal/application/attach"
	"micrun/internal/ports"
)

func NewService(ioFactory ports.IOSessionFactory, opts ...Option) *Service {
	attach, err := attachapp.NewServiceChecked(ioFactory)
	if err != nil {
		panic(err)
	}
	service, err := NewServiceChecked(attach, opts...)
	if err != nil {
		panic(err)
	}
	return service
}
