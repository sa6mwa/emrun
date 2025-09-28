# emrun

Run embedded executables and scripts straight from anonymous memory on Linux and
Android, or from temporary files on any platform. `emrun` wraps
`memfd_create(2)` so you can bundle auxiliary tooling and scripts inside a Go
binary, execute them without touching disk in the common case, and keep the
package fully self-contained even when hardened kernels restrict anonymous
execution. The companion package `efrun` exposes the same API surface but always
executes from a temporary file, which makes it portable to platforms where
`memfd_create` is unavailable, but you have to explicitly choose that import.

## Features

- Creates an anonymous executable file descriptor whose name is the payload
  SHA-256, making it easy to trace in `/proc/<pid>/fd`.
- Runs raw byte payloads, inline scripts, or strings with `Run`, `RunIO`,
  `RunIOE`, and `Do`.
- Works seamlessly with Go's `//go:embed`, enabling you to ship extra binaries
  or shell helpers in a single distributable.
- Automatically falls back to a temporary on-disk executable (deleted on close)
  when security policies forbid running the anonymous file descriptor.
- Swap in `efrun` when you need the same interface without memfd support—for
  example on Windows or macOS, or when policies forbid anonymous execution and
  you prefer to opt out of the memfd attempt entirely.

## Installation

```
go get github.com/sa6mwa/emrun
```

The `emrun` package itself only builds on Linux and Android (see the build tag
at the top of `emrun.go`). The module also provides
`github.com/sa6mwa/emrun/efrun`, which compiles on any platform and mirrors the
same helpers without using `memfd_create`.

## Quick Start

Below is the smallest example. The payload here is an inline shell script, but
all helpers return the same experience even when backed by `//go:embed`.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/sa6mwa/emrun"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    payload := []byte("#!/bin/sh\necho quick-start\n")

    out, err := emrun.Run(ctx, payload)
    if err != nil {
        log.Fatalf("run failed: %v", err)
    }
    fmt.Printf("payload said: %s", out)
}
```

## Using `//go:embed` With Binaries

The primary use-case is bundling helper binaries so that the top-level Go
program ships as a single artifact. Start by embedding your binary as a byte
slice and then execute it with `Run` or `RunIO`.

```go
package main

import (
    "context"
    "embed"
    "fmt"
    "log"
    "time"

    "github.com/sa6mwa/emrun"
)

//go:embed bin/busybox
var busybox []byte

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    // BusyBox expects the applet name as the first argument when invoked as
    // `busybox <applet>`. The memfd path will still be argv[0].
    out, err := emrun.Run(ctx, busybox, "echo", "hello from busybox")
    if err != nil {
        log.Fatalf("busybox run failed: %v", err)
    }

    fmt.Printf("busybox output:\n%s", out)
}
```

Notes:
- The payload must be executable for Linux; if you embed an ELF binary, ensure
  it targets the same architecture as the host machine.
- `Run` returns combined stdout and stderr. `RunIO` mirrors both streams into a
  single writer, while `RunIOE` accepts separate writers for stdout and stderr.
- When a fallback is required, the payload is written to a `0700` temporary
  file under the current user's default temp directory. `Close()` removes it.

## Embedding Scripts With `//go:embed`

Shell scripts can also be embedded, which keeps helper logic tidy and separate
from the Go source.

```go
package script

import (
    "context"
    "embed"
    "log"
    "os"
    "time"

    "github.com/sa6mwa/emrun"
)

//go:embed scripts/upgrade.sh
var upgradeScript []byte

func RunUpgrade(ctx context.Context, args ...string) error {
    // Stream output directly to the parent process so progress is visible.
    return emrun.RunIO(ctx, nil, os.Stdout, upgradeScript, args...)
}
```

From the caller you can pass a context with a deadline or cancel when shutting
down.

## Inline Script Helpers

Sometimes writing the payload in Go is more convenient. `Do` accepts a string,
which is ideal for small wrappers or ad-hoc tasks.

```go
out, err := emrun.Do(
    ctx, `#!/bin/sh
now=$(date +%s)
echo "inline timestamp: $now"
`)
if err != nil {
    return err
}
fmt.Print(string(out))
```

For more control, use `RunIO` with custom readers/writers. The example below
pipes data in and captures combined output through a single buffer. Choose
`RunIOE` when you need to keep stdout and stderr separate without shell tricks.

```go
var combined bytes.Buffer
err := emrun.RunIO(
    ctx,
    strings.NewReader("payload\n"),
    &combined,
    []byte("#!/bin/sh\nread line\nprintf 'seen:%s\\n' \"$line\"\n"),
)
if err != nil {
    return err
}
log.Printf("script said %q", combined.String())
```

## Selecting a Runner

Both runners expose the same helpers: `Open`, `Run`, `RunIO`, `RunIOE`, and `Do`.
Switching between them is one import change, making it easy to choose the
execution strategy per build.

- `github.com/sa6mwa/emrun` (Linux/Android only) prefers anonymous execution via
  `memfd_create` and auto-falls back to a secure temporary file when necessary.
- `github.com/sa6mwa/emrun/efrun` (portable) always writes a temporary file.

Internally, both return a `port.Runnable` with a shared `Run` method so you can
depend on the interface in your own abstractions.

### Build-Tagged Imports

If your program targets both Linux/Android and other operating systems, make the
runner choice explicit with build tags. The `emrun` package will not compile on
non-Linux targets, so cross-compiles fail unless you import `efrun` on those
platforms (this was a design-choice). The pattern below keeps call sites
identical while letting the Go toolchain select the correct backend
automatically:

```go
// runner_linux.go
//go:build linux || android
// +build linux android

package runner

import (
    runner "github.com/sa6mwa/emrun"
)
```

```go
// runner_other.go
//go:build !linux && !android
// +build !linux,!android

package runner

import (
    runner "github.com/sa6mwa/emrun/efrun"
)
```

Everywhere else, depend on the shared `runner` alias (or whichever name you
prefer) and call `runner.Run`, `runner.Do`, etc. When you build with
`GOOS=linux` or `GOOS=android`, the first file is compiled; all other targets
use the second.

## Execution Flow (emrun)

1. `Open` calls `memfd_create` with the SHA-256 digest as the name, writes the
   payload into the anonymous file, and returns a handle pointing at
   `/proc/self/fd/<n>`.
2. `Run`, `RunIO`, and `Do` call `Open`, execute using `exec.CommandContext`,
   and close the file descriptor after the process exits.
3. If the kernel refuses to execute the anonymous file (for example SELinux or
   AppArmor report `EACCES`/`EPERM`), the payload is atomically copied to a
   secure temporary file, permissioned `0700`, and the command is retried from
   disk. Temporary files are removed automatically on `Close()`.

Because the backing file exists only in memory in the common case, nothing ever
hits disk. The file descriptor is closed immediately after successful execution,
keeping the footprint small. When you import `efrun`, only step 3 applies—the
payload is written to a temporary file immediately and executed from disk.

## Caveats and Platform Notes

- **SELinux / AppArmor**: Some distributions ship with policies that forbid
  executing anonymous memory (for example `execmem`, `execmod`, or
  `memfd_exec`). Tight policies may block the child process from starting or log
  AVC denials. `emrun` detects permission denials and retries from a temporary
  file automatically, but you should still validate under the target policy and
  ensure the temp directory is acceptable for your threat model.
- **Android**: Android kernels support `memfd_create`, but application sandboxes
  and `seccomp` filters can forbid executing anonymous memory. Apps running in
  the default untrusted app domain may hit permission denials, so verify on the
  minimum API level you need.
- **Kernel version**: `memfd_create` requires Linux 3.17+. Older LTS kernels or
  restricted containers may not provide it. The package surfaces the original
  error from `unix.MemfdCreate` so you can detect unsupported hosts.
- **Debugging and tooling**: Since payloads never land on disk, external tools
  cannot read them post-mortem. Be sure to keep a copy of the original assets if
  you rely on crash dumps or static analysis.
- **Resource management**: `Open` callers must close the returned file when they
  stop using it. The helper functions handle this automatically, but remember to
  close files when using `Open` directly.

## License
MIT
