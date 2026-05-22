package attach

import "micrun/internal/ports"

func NewService(ioFactory ports.IOSessionFactory, opts ...Option) *Service {
	service, err := NewServiceChecked(ioFactory, opts...)
	if err != nil {
		panic(err)
	}
	return service
}
