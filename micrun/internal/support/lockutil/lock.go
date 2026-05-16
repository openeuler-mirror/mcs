package lockutil

type Locker interface {
	Lock()
	Unlock()
}

type ReadLocker interface {
	RLock()
	RUnlock()
}

func WithLock(locker Locker, body func()) {
	locker.Lock()
	defer locker.Unlock()
	body()
}

func WithLockError(locker Locker, body func() error) error {
	locker.Lock()
	defer locker.Unlock()
	return body()
}

func WithLockValue[T any](locker Locker, body func() T) T {
	locker.Lock()
	defer locker.Unlock()
	return body()
}

func WithReadLock(locker ReadLocker, body func()) {
	locker.RLock()
	defer locker.RUnlock()
	body()
}

func WithReadLockValue[T any](locker ReadLocker, body func() T) T {
	locker.RLock()
	defer locker.RUnlock()
	return body()
}
