package console

// OutputConfig configures RTOS console output normalization.
type OutputConfig struct {
	FilterNUL           bool
	CompressLineEndings bool
}
