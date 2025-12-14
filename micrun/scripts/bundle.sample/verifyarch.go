package main

import (
    "debug/elf"
    "errors"
    "flag"
    "fmt"
    "os"
    "path/filepath"
    "runtime"
)

// verifyELFArch opens the file and checks if its ELF machine matches host arch.
func verifyELFArch(path string) (bool, string, error) {
    f, err := elf.Open(path)
    if err != nil {
        // Not an ELF is not an error here; caller can try other checks.
        return false, "not-elf", nil
    }
    defer f.Close()

    var ok bool
    switch runtime.GOARCH {
    case "arm64":
        ok = f.Machine == elf.EM_AARCH64
    case "amd64":
        ok = f.Machine == elf.EM_X86_64
    case "arm":
        ok = f.Machine == elf.EM_ARM
    case "riscv64":
        ok = f.Machine == elf.EM_RISCV
    default:
        return false, f.Machine.String(), errors.New("unsupported host arch: " + runtime.GOARCH)
    }
    return ok, f.Machine.String(), nil
}

// quickARM64RawHeuristic returns true if the file looks like an ARM64 raw Image.
func quickARM64RawHeuristic(path string) bool {
    // Many arm64 raw Images have the magic text "ARMd" near the header.
    // This is a best-effort heuristic used previously in micran.
    fh, err := os.Open(path)
    if err != nil {
        return false
    }
    defer fh.Close()
    buf := make([]byte, 0x40)
    if _, err := fh.Read(buf); err != nil {
        return false
    }
    for i := 0; i+3 < len(buf); i++ {
        if buf[i] == 'A' && buf[i+1] == 'R' && buf[i+2] == 'M' && buf[i+3] == 'd' {
            return true
        }
    }
    return false
}

func main() {
    flag.Usage = func() {
        fmt.Fprintf(os.Stderr, "Usage: %s <file>\n", filepath.Base(os.Args[0]))
        flag.PrintDefaults()
    }
    flag.Parse()
    if flag.NArg() != 1 {
        flag.Usage()
        os.Exit(2)
    }
    path := flag.Arg(0)

    // Step 1: try ELF check
    ok, machine, err := verifyELFArch(path)
    if err != nil {
        fmt.Fprintf(os.Stderr, "error: %v\n", err)
        os.Exit(1)
    }
    if ok {
        fmt.Printf("OK (ELF %s matches host %s)\n", machine, runtime.GOARCH)
        return
    }
    if machine != "not-elf" {
        fmt.Printf("MISMATCH (ELF %s vs host %s)\n", machine, runtime.GOARCH)
        os.Exit(3)
    }

    // Step 2: non-ELF; apply simple raw ARM64 heuristic when host is arm64.
    if quickARM64RawHeuristic(path) {
        fmt.Printf("OK (raw arm64 image heuristic)\n")
        return
    }

    fmt.Printf("UNKNOWN (non-ELF; no match heuristic)\n")
}
