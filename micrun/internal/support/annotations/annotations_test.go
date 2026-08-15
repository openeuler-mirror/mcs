package annotations

import "testing"

// These constants are external wire contracts: they appear in ctr/nerdctl
// command lines, image builder annotations, and Kubernetes pod specs. A
// rename here silently breaks every external caller, so the exact literal
// values are pinned by this table. If a key must change, do it through a
// deprecation cycle (see OldAutoCloseTimeout) and update this table in the
// same commit.
func TestAnnotationLiteralsPinned(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"MicrunAnnotationPrefix", MicrunAnnotationPrefix, "org.openeuler.micrun."},
		{"PedPrefix", PedPrefix, "org.openeuler.micrun.ped."},
		{"RuntimePrefix", RuntimePrefix, "org.openeuler.micrun.runtime."},
		{"ContainerPrefix", ContainerPrefix, "org.openeuler.micrun.container."},
		{"CompatPrefix", CompatPrefix, "org.openeuler.micrun.compatibility."},

		{"BundlePathKey", BundlePathKey, "org.openeuler.micrun.pkg.oci.bundle_path"},
		{"ContainerTypeKey", ContainerTypeKey, "org.openeuler.micrun.pkg.oci.container_type"},
		{"SandboxConfigPathKey", SandboxConfigPathKey, "org.openeuler.micrun.config_path"},

		{"OSAnnotation", OSAnnotation, "org.openeuler.micrun.container.os"},
		{"FirmwarePathAnno", FirmwarePathAnno, "org.openeuler.micrun.container.firmware_path"},
		{"FirmwareHash", FirmwareHash, "org.openeuler.micrun.container.firmware_hash"},
		{"AutoClose", AutoClose, "org.openeuler.micrun.container.auto_close"},
		{"AutoCloseTimeout", AutoCloseTimeout, "org.openeuler.micrun.container.auto_close_timeout"},
		{"OldAutoCloseTimeout", OldAutoCloseTimeout, "org.openeuler.micrun.container.auto_disconnect_timeout"},
		{"Pedtype", Pedtype, "org.openeuler.micrun.ped.pedestal"},
		{"PedCompat", PedCompat, "org.openeuler.micrun.ped.compatibility"},
		{"NetPlaceholder", NetPlaceholder, "org.openeuler.micrun.ped.net_placeholder"},
		{"PedestalConf", PedestalConf, "org.openeuler.micrun.ped.conf"},

		{"ContainerMinMemMB", ContainerMinMemMB, "org.openeuler.micrun.container.min_memory_mb"},
		{"ContainerMaxVcpuNum", ContainerMaxVcpuNum, "org.openeuler.micrun.container.max_vcpu_num"},

		{"DisableNewNetNs", DisableNewNetNs, "org.openeuler.micrun.runtime.disable_new_netns"},
		{"Experimental", Experimental, "org.openeuler.micrun.runtime.experimental"},
		{"PipeSize", PipeSize, "org.openeuler.micrun.runtime.pipe_size"},
		{"RuntimeDebug", RuntimeDebug, "org.openeuler.micrun.runtime.debug"},
		{"RuntimeMaxContainerCPUs", RuntimeMaxContainerCPUs, "org.openeuler.micrun.runtime.max_container_cpus"},
		{"RuntimeMaxContainerMemory", RuntimeMaxContainerMemory, "org.openeuler.micrun.runtime.max_container_memory"},
		{"RuntimePauseImage", RuntimePauseImage, "org.openeuler.micrun.runtime.pause"},
		{"RuntimeExclusiveDom0CPU", RuntimeExclusiveDom0CPU, "org.openeuler.micrun.runtime.exclusive_dom0_cpu"},
		{"RuntimeEnableVCPUsPinning", RuntimeEnableVCPUsPinning, "org.openeuler.micrun.runtime.enable_vcpus_pinning"},
		{"RuntimeStaticResource", RuntimeStaticResource, "org.openeuler.micrun.runtime.static_resource"},
		{"RuntimeHugePageEnable", RuntimeHugePageEnable, "org.openeuler.micrun.runtime.hugepage_enable"},
		{"VCPUBinding", VCPUBinding, "org.openeuler.micrun.runtime.vcpu_pcpu_binding"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}
