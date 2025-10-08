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
- Enforces context-driven SHA-256 allow/deny policies so bundled or perhaps
  downloaded payloads obey explicit checksum rules before execution.
- Swap in `efrun` when you need the same interface without memfd support—for
  example on Windows or macOS, or when policies forbid anonymous execution and
  you prefer to opt out of the memfd attempt entirely.

## Installation

```
go get pkt.systems/emrun
```

The `emrun` package itself only builds on Linux and Android (see the build tag
at the top of `emrun.go`). The module also provides
`pkt.systems/emrun/efrun`, which compiles on any platform and mirrors the
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

    "pkt.systems/emrun"
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

    "pkt.systems/emrun"
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

    "pkt.systems/emrun"
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

## Enforcing SHA-256 Policies

`emrun` can restrict which embedded payloads run by attaching digest policies to
the `context.Context`. Set a default verdict with `WithPolicy`, then register
explicit allow or deny entries with `WithRule`. Inputs may be raw hex strings,
`[32]byte` values, or the contents of a `sha256sum` file—filenames are ignored.

```go
ctx := emrun.WithPolicy(ctx, emrun.DENY) // default to deny unknown payloads

// Load hashes from an embedded sha256sum file or explicit strings.
ctx = emrun.WithRule(ctx, emrun.ALLOW, embeddedChecksums, "b09864fcb9...")

// Run helpers consult the policy automatically.
out, err := emrun.Run(ctx, payload)

// Manual checks are available when wiring custom execution paths.
digest := sha256.Sum256(payload)
if err := emrun.CheckPolicy(ctx, digest, hex.EncodeToString(digest[:])); err != nil {
    return err
}
```

Use `WithRuleCatchError` when you need to surface parse errors instead of
panicking—handy if the checksum material comes from user input or config files.

## Background Execution

`RunBG`, `RunIOBG`, `RunIOEBG`, and `DoBG` launch payloads asynchronously and
return a `Background` handle. The handle exposes the running `Context`, a
`Cancel` function, and a `Done` channel that delivers a `Result`. Combined
output is only captured for `RunBG`/`DoBG`; streaming variants return `nil` in
`Result.CombinedOutput` because stdout/stderr are already wired to callers.

Simple example that runs one payload in the background and waits for it:

```go
bg, err := emrun.RunBG(ctx, payload, "--task")
if err != nil {
    return err
}
defer bg.Cancel()

select {
case res := <-bg.Done:
    if res.Error != nil {
        return res.Error
    }
    fmt.Printf("exit=%d output=%s", res.ExitCode, res.CombinedOutput)
case <-ctx.Done():
    return ctx.Err()
}
```

Running multiple background commands concurrently:

```go
bg1, err := emrun.RunBG(ctx, payload1)
if err != nil {
    return err
}
bg2, err := emrun.RunIOBG(ctx, nil, os.Stdout, payload2)
if err != nil {
    bg1.Cancel()
    return err
}
defer bg1.Cancel()
defer bg2.Cancel()

results := make(chan *emrun.Result, 2)
collect := func(name string, bg *emrun.Background) {
    res := bg.WaitWithContext(ctx)
    if res.Error != nil {
        log.Printf("%s failed: %v", name, res.Error)
    } else {
        log.Printf("%s exit=%d", name, res.ExitCode)
    }
    results <- &res
}

go collect("job1", bg1)
go collect("job2", bg2)

var firstErr error
for i := 0; i < 2; i++ {
    res := <-results
    if res.Error != nil && firstErr == nil {
        firstErr = res.Error
    }
}
if firstErr != nil {
    return firstErr
}
```

Or coordinating a dynamic set of background jobs:

```go
backgrounds := make([]*emrun.Background, 0, len(payloads))
for i, payload := range payloads {
    bg, err := emrun.RunBG(ctx, payload, fmt.Sprintf("--job=%d", i))
    if err != nil {
        for _, b := range backgrounds {
            b.Cancel()
        }
        return err
    }
    backgrounds = append(backgrounds, bg)
}
defer func() {
    for _, bg := range backgrounds {
        bg.Cancel()
    }
}()

for _, bg := range backgrounds {
    select {
    case res := <-bg.Done:
        if res.Error != nil {
            return res.Error
        }
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

The same helpers exist in `efrun` if you need the portable backend.

## Selecting a Runner

Both runners expose the same helpers: `Open`, `Run`, `RunIO`, `RunIOE`, `Do`,
and their background equivalents. Switching between them is one import change,
making it easy to choose the execution strategy per build.

- `pkt.systems/emrun` (Linux/Android only) prefers anonymous execution via
  `memfd_create` and auto-falls back to a secure temporary file when necessary.
- `pkt.systems/emrun/efrun` (portable) always writes a temporary file.

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
    runner "pkt.systems/emrun"
)
```

```go
// runner_other.go
//go:build !linux && !android
// +build !linux,!android

package runner

import (
    runner "pkt.systems/emrun/efrun"
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
