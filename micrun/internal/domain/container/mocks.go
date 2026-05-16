package container

type dummyNetwork struct{}

func (dn *dummyNetwork) NetworkIsCreated() bool {
	return true
}

func (dn *dummyNetwork) NetID() string {
	return "dummy"
}

func (dn *dummyNetwork) NetworkCleanup(id string) error {
	return nil
}
