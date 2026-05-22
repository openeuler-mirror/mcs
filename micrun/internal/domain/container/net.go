package container

import "micrun/internal/support/netns"

type NetworkConfig struct {
	NetworkID      string `json:"network_id"`
	NetworkCreated bool   `json:"network_created"`
	HolderPid      int    `json:"holder_pid,omitempty"`
}

func (n *NetworkConfig) NetworkIsCreated() bool {
	if n == nil {
		return false
	}
	return n.NetworkCreated
}

func (n *NetworkConfig) NetworkCleanup(id string) error {
	if n == nil {
		return nil
	}

	if err := netns.Cleanup(id, n.HolderPid); err != nil {
		return err
	}

	n.NetworkID = ""
	n.NetworkCreated = false
	n.HolderPid = 0
	return nil
}

func (n *NetworkConfig) NetID() string {
	if n == nil {
		return ""
	}
	return n.NetworkID
}
