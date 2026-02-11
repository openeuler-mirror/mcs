# Newline Fix Summary

## Problem

When running `ctr task start` with an RTOS container, pressing Enter would produce extra blank lines:
- After "Hello, UniProton!" there were 3+ blank lines
- Pressing Enter showed prompt + extra blank line
- Help command output had excessive blank lines

## Root Cause

Two issues were identified:

1. **TTY Output Processing**: The `ONLCR` termios flag was converting `\n` to `\r\n`, but the RTOS firmware already sent `\r\n`, resulting in `\r\r\n` which displays as an extra blank line.

2. **RTOS Firmware Output**: The RTOS firmware itself outputs multiple consecutive `\r\n` sequences.

## Solution

### Fix 1: Disable TTY Output Processing (pkg/micantainer/rpmsg_tty.go)

Disabled `OPOST|ONLCR` flags to make TTY pass through data transparently:

```go
// Disable OPOST and all output processing
// RTOS already sends proper line endings (\r\n)
termios.Oflag &^= unix.OPOST | unix.ONLCR | unix.OCRNL | unix.OLCUC | unix.OCRNL
```

### Fix 2: Compress Consecutive Line Endings (pkg/io/copier.go)

Added `compressLineEndings()` function to compress multiple consecutive `\r\n` or `\n` sequences to a single `\r\n`:

```go
func compressLineEndings(data []byte) []byte {
    // Compresses consecutive line endings to a single CRLF
    // Handles RTOS firmware that outputs multiple \r\n
}
```

Integrated into `copyStdout()`:
```go
// Compress consecutive line endings
data = compressLineEndings(data)
```

## Testing

### Test Script

Run the verification test on the remote host:

```bash
# On 192.168.7.2:
bash tests/io/test_newline_fix_verify.sh
```

### Expected Output

**Before fix:**
```
Hello, UniProton!



openEuler UniProton #
```

**After fix:**
```
Hello, UniProton! 

openEuler UniProton #
```

Note: After "Hello" there is only ONE blank line (normal) instead of 3+ blank lines (excessive).

### Verification Results

Test output from `test_nl_simple.sh`:
```
Hello, UniProton! 

openEuler UniProton #
```

✓ **PASS**: Only 1 blank line after Hello (expected: 1-2, was: 3+ before fix)

## Related Files

- `pkg/micantainer/rpmsg_tty.go` - TTY configuration fix
- `pkg/io/copier.go` - Line ending compression
- `tests/io/test_newline_fix_verify.sh` - Verification test script
- `tests/io/test_newline_comprehensive.exp` - Comprehensive expect test
- `tests/io/test_with_script.exp` - Script-based test

## Git Commits

- `0c4c803` - micrun/rpmsg_tty: disable ONLCR to fix extra blank lines
- `f0bae2f` - micrun/io: add compressLineEndings to handle RTOS line endings
- `b48ba78` - micrun/docs: add newline fix testing documentation
