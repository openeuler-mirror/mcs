package configstack

// ContainerLayer captures overrides sourced from client.conf or similar files.
type ContainerLayer struct {
	ImageAbsPath string
	PedestalType string
	PedestalConf string
	OS           string
}
