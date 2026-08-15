// Command containerd_client drives containerd Task service operations that
// the ctr CLI does not expose, for the micrun QEMU feature tests.
//
// Currently supported:
//
//	containerd_client update --id <id> [--memory-mb N] [--cpus N] [--cpuset "0-1"]
//	    Calls Task.Update on a running task with a LinuxResources payload —
//	    the same RPC CRI's UpdateContainerResources routes through, which is
//	    the only production driver of micrun's hot resource update path.
//
//	containerd_client run --image <ref> --id <id> [--memory-mb N] [--cpus N] [--annotation k=v]...
//	    Creates and starts a container with LinuxResources embedded in the
//	    OCI spec (the same spec field CRI/kubelet populate from pod resources).
//	    Bypasses nerdctl's registry resolution — the image must already be
//	    imported into containerd's local store (e.g. via ctr images import).
package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
)

const defaultAddress = "/run/containerd/containerd.sock"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "update":
		update(os.Args[2:])
	case "run":
		runContainer(os.Args[2:])
	case "pause":
		pauseTask(os.Args[2:])
	case "resume":
		resumeTask(os.Args[2:])
	case "metrics":
		metricsTask(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: containerd_client <update|run> [flags]")
	fmt.Fprintln(os.Stderr, "  update --id <id> [--address sock] [--namespace ns] [--memory-mb N] [--cpus N] [--cpuset 0-1]")
	fmt.Fprintln(os.Stderr, "  run --image <ref> --id <id> [--address sock] [--namespace ns] [--memory-mb N] [--cpus N] [--annotation k=v]...")
	fmt.Fprintln(os.Stderr, "  pause --id <id>")
	fmt.Fprintln(os.Stderr, "  resume --id <id>")
	fmt.Fprintln(os.Stderr, "  metrics --id <id>")
}

func update(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	address := fs.String("address", defaultAddress, "containerd socket address")
	namespace := fs.String("namespace", "default", "containerd namespace")
	id := fs.String("id", "", "container id")
	memoryMB := fs.Uint64("memory-mb", 0, "memory limit in MiB (0 = leave unchanged)")
	cpus := fs.Float64("cpus", 0, "CPU count as quota/period (0 = leave unchanged)")
	cpuset := fs.String("cpuset", "", "cpuset cpus (empty = leave unchanged)")
	_ = fs.Parse(args)

	if *id == "" || (*memoryMB == 0 && *cpus == 0 && *cpuset == "") {
		usage()
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(namespaces.WithNamespace(context.Background(), *namespace), 60*time.Second)
	defer cancel()

	client, err := containerd.New(*address)
	if err != nil {
		fatal("connect containerd: %v", err)
	}
	defer client.Close()

	container, err := client.LoadContainer(ctx, *id)
	if err != nil {
		fatal("load container %s: %v", *id, err)
	}
	task, err := container.Task(ctx, nil)
	if err != nil {
		fatal("load task for %s: %v", *id, err)
	}

	var res specs.LinuxResources
	if *memoryMB > 0 {
		res.Memory = &specs.LinuxMemory{Limit: int64Ptr(int64(*memoryMB) * 1024 * 1024)}
	}
	if *cpus > 0 {
		period := uint64Ptr(100000)
		// math.Round: float64 truncation turned 2.3*1e5 into 229999, and the
		// shim's quota->capacity conversion amplifies the shortfall ~100x
		// into the guest CPU cap (2.3 vCPUs became 2.99).
		quota := int64Ptr(int64(math.Round(*cpus * 100000)))
		res.CPU = &specs.LinuxCPU{Period: period, Quota: quota}
	}
	if *cpuset != "" {
		if res.CPU == nil {
			res.CPU = &specs.LinuxCPU{}
		}
		res.CPU.Cpus = *cpuset
	}

	if err := task.Update(ctx, containerd.WithResources(&res)); err != nil {
		fatal("update resources for %s: %v", *id, err)
	}
	fmt.Printf("update ok: id=%s memory_mb=%d cpus=%g cpuset=%q\n", *id, *memoryMB, *cpus, *cpuset)
}

func int64Ptr(v int64) *int64    { return &v }
func uint64Ptr(v uint64) *uint64 { return &v }

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "containerd_client: "+format+"\n", args...)
	os.Exit(1)
}

// annotationFlags is a custom flag.Value collecting repeated --annotation k=v pairs.
type annotationFlags []string

func (a *annotationFlags) String() string { return strings.Join(*a, ",") }
func (a *annotationFlags) Set(v string) error {
	*a = append(*a, v)
	return nil
}

func runContainer(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	address := fs.String("address", defaultAddress, "containerd socket address")
	namespace := fs.String("namespace", "default", "containerd namespace")
	id := fs.String("id", "", "container id")
	imageRef := fs.String("image", "", "image reference (must exist in containerd store)")
	memoryMB := fs.Uint64("memory-mb", 0, "memory limit in MiB (0 = omit)")
	cpus := fs.Float64("cpus", 0, "CPU count as quota/period (0 = omit)")
	cpuset := fs.String("cpuset", "", "cpuset cpus (empty = omit)")
	runtime := fs.String("runtime", "", "containerd runtime name (empty = default)")
	var annotations annotationFlags
	fs.Var(&annotations, "annotation", "container annotation k=v (repeatable)")

	_ = fs.Parse(args)

	if *id == "" || *imageRef == "" {
		fatal("run: --id and --image are required")
	}
	if *runtime == "" {
		fatal("run: --runtime is required (e.g. io.containerd.mica.v2)")
	}

	ctx, cancel := context.WithTimeout(namespaces.WithNamespace(context.Background(), *namespace), 120*time.Second)
	defer cancel()

	client, err := containerd.New(*address)
	if err != nil {
		fatal("connect containerd: %v", err)
	}
	defer client.Close()

	image, err := client.GetImage(ctx, *imageRef)
	if err != nil {
		fatal("get image %s (must be imported first): %v", *imageRef, err)
	}

	// Build a minimal OCI spec the way kubelet does for CRI pods:
	// extract the image annotations, add the pod's resource limits, and
	// hand the spec to containerd — no rootfs generation needed for
	// firmware images. WithNewSpec(WithImageConfig) would fail here
	// because it tries to resolve the rootfs layer.
	spec := &oci.Spec{
		Version:     "1.0.2",
		Annotations: map[string]string{},
	}
	if *memoryMB > 0 || *cpus > 0 || *cpuset != "" {
		spec.Linux = &specs.Linux{
			Resources: &specs.LinuxResources{
				Memory: linuxMemory(*memoryMB),
				CPU:    linuxCPU(*cpus, *cpuset),
			},
		}
	}
	for _, kv := range annotations {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			fatal("invalid --annotation %q (want k=v)", kv)
		}
		spec.Annotations[parts[0]] = parts[1]
	}

	container, err := client.NewContainer(ctx, *id,
		containerd.WithImage(image),
		containerd.WithSpec(spec),
		containerd.WithRuntime(*runtime, nil),
		containerd.WithNewSnapshot(*id, image),
	)
	if err != nil {
		fatal("create container %s: %v", *id, err)
	}

	task, err := container.NewTask(ctx, cio.NewCreator(cio.WithStdio))
	if err != nil {
		container.Delete(ctx)
		fatal("create task for %s: %v", *id, err)
	}
	if err := task.Start(ctx); err != nil {
		task.Delete(ctx)
		container.Delete(ctx)
		fatal("start task %s: %v", *id, err)
	}

	st, err := task.Status(ctx)
	if err == nil {
		fmt.Printf("run ok: id=%s status=%s pid=%d\n", *id, st.Status, task.Pid())
	} else {
		fmt.Printf("run ok: id=%s (status unavailable: %v)\n", *id, err)
	}
}

func linuxMemory(memoryMB uint64) *specs.LinuxMemory {
	if memoryMB == 0 {
		return nil
	}
	limit := int64(memoryMB * 1024 * 1024)
	return &specs.LinuxMemory{Limit: &limit}
}

func linuxCPU(cpus float64, cpuset string) *specs.LinuxCPU {
	if cpus == 0 && cpuset == "" {
		return nil
	}
	var cpu specs.LinuxCPU
	if cpus > 0 {
		period := uint64(100000)
		quota := int64(math.Round(cpus * 100000))
		cpu.Period = &period
		cpu.Quota = &quota
	}
	if cpuset != "" {
		cpu.Cpus = cpuset
	}
	return &cpu
}

// pauseTask pauses a running task (the CRI PauseContainer path).
func pauseTask(args []string) {
	fs := flag.NewFlagSet("pause", flag.ExitOnError)
	address := fs.String("address", defaultAddress, "containerd socket address")
	namespace := fs.String("namespace", "default", "containerd namespace")
	id := fs.String("id", "", "container id")
	_ = fs.Parse(args)
	if *id == "" {
		fatal("pause: --id is required")
	}
	withTask(*address, *namespace, *id, func(ctx context.Context, task containerd.Task) error {
		return task.Pause(ctx)
	})
	fmt.Printf("pause ok: id=%s\n", *id)
}

// resumeTask resumes a paused task (the CRI ResumeContainer path).
func resumeTask(args []string) {
	fs := flag.NewFlagSet("resume", flag.ExitOnError)
	address := fs.String("address", defaultAddress, "containerd socket address")
	namespace := fs.String("namespace", "default", "containerd namespace")
	id := fs.String("id", "", "containerd id")
	_ = fs.Parse(args)
	if *id == "" {
		fatal("resume: --id is required")
	}
	withTask(*address, *namespace, *id, func(ctx context.Context, task containerd.Task) error {
		return task.Resume(ctx)
	})
	fmt.Printf("resume ok: id=%s\n", *id)
}

// metricsTask fetches task metrics (the CRI/ctr task metrics path).
func metricsTask(args []string) {
	fs := flag.NewFlagSet("metrics", flag.ExitOnError)
	address := fs.String("address", defaultAddress, "containerd socket address")
	namespace := fs.String("namespace", "default", "containerd namespace")
	id := fs.String("id", "", "containerd id")
	_ = fs.Parse(args)
	if *id == "" {
		fatal("metrics: --id is required")
	}
	withTask(*address, *namespace, *id, func(ctx context.Context, task containerd.Task) error {
		mt, err := task.Metrics(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("metrics ok: id=%s type=%s\n", *id, mt.ID)
		return nil
	})
}

// withTask connects, loads the container's task, and invokes fn.
func withTask(address, namespace, id string, fn func(context.Context, containerd.Task) error) {
	ctx, cancel := context.WithTimeout(namespaces.WithNamespace(context.Background(), namespace), 30*time.Second)
	defer cancel()
	client, err := containerd.New(address)
	if err != nil {
		fatal("connect containerd: %v", err)
	}
	defer client.Close()
	container, err := client.LoadContainer(ctx, id)
	if err != nil {
		fatal("load container %s: %v", id, err)
	}
	task, err := container.Task(ctx, nil)
	if err != nil {
		fatal("load task for %s: %v", id, err)
	}
	if err := fn(ctx, task); err != nil {
		fatal("operation on %s: %v", id, err)
	}
}
