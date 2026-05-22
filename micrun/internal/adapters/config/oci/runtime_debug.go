package oci

func (r *RuntimeConfig) SetDebug(debugStr string) {
	debug, ok := parseRuntimeBool(KeyDebug, debugStr)
	if !ok {
		return
	}
	r.Debug = debug
}
