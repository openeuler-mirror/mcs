package pedestal

import "testing"

func TestHostPedestalDetectorDetect(t *testing.T) {
	tests := []struct {
		name      string
		mock      bool
		xen       bool
		baremetal bool
		want      PedType
	}{
		{name: "mock uses xen", mock: true, want: Xen},
		{name: "xen wins before baremetal", xen: true, baremetal: true, want: Xen},
		{name: "baremetal when xen missing", baremetal: true, want: Baremetal},
		{name: "unsupported when no detector matches", want: Unsupported},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := hostPedestalDetector{
				isMock: func() bool {
					return tt.mock
				},
				isXen: func() bool {
					return tt.xen
				},
				isBaremetal: func() bool {
					return tt.baremetal
				},
			}

			if got := detector.detect(); got != tt.want {
				t.Fatalf("detect() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestDetectBaremetalRequiresFeatureFlag(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "empty disabled", value: "", want: false},
		{name: "zero disabled", value: "0", want: false},
		{name: "false disabled", value: "false", want: false},
		{name: "no disabled", value: "no", want: false},
		{name: "one enabled", value: "1", want: true},
		{name: "yes enabled", value: "yes", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(baremetalDetectionEnv, tt.value)
			if got := detectBaremetal(); got != tt.want {
				t.Fatalf("detectBaremetal() = %v, want %v", got, tt.want)
			}
		})
	}
}
