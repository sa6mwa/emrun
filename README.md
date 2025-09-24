# emrun

Run embedded executables and scripts straight from anonymous memory on Linux and
Android. `emrun` wraps `memfd_create(2)` so you can bundle auxiliary tooling and
scripts inside a Go binary, execute them without touching disk, and keep the
package fully self-contained.

## Features
- Creates an anonymous executable file descriptor whose name is the payload
  SHA-256, making it easy to trace in `/proc/<pid>/fd`.
- Runs raw byte payloads, inline scripts, or strings with `Run`, `RunIO`, and `Do`.
- Works seamlessly with Go's `//go:embed`, enabling you to ship extra binaries or
  shell helpers in a single distributable.

## Installation
```
go get github.com/sa6mwa/emrun
```

The module only builds on Linux and Android (see the build tag at the top of
`emrun.go`).

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

    // BusyBox accepts the applet as argv[0], followed by its own arguments.
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
- `Run` returns combined stdout and stderr; if you need separate streams, use
  `RunIO` and provide distinct `io.Writer`s.

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
pipes data in and captures combined output through a single buffer.

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

## Execution Flow
1. `Open` calls `memfd_create` with the SHA-256 digest as the name, writes the
   payload into the anonymous file, and returns an `*os.File` pointing at
   `/proc/self/fd/<n>`.
2. `Run`, `RunIO`, and `Do` call `Open`, execute using `exec.CommandContext`,
   and close the file descriptor after the process exits.

Because the backing file exists only in memory, nothing ever hits disk. The file
descriptor is closed immediately after execution, keeping the footprint small.

## Caveats and Platform Notes
- **SELinux / AppArmor**: Some distributions ship with policies that forbid
  executing anonymous memory (for example `execmem`, `execmod`, or
  `memfd_exec`). Tight policies may block the child process from starting or log
  AVC denials. When targeting locked-down systems, test under the same policy
  and be prepared to ship an alternative fallback (e.g. writing to `/tmp`).
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
