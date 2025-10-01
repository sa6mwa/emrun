# Repository Guidelines

Use this guide to align `emrun` and `efrun` changes with existing build, test, and review patterns before opening a pull request.

## Project Structure & Module Organization
- Root package (`emrun.go`, `executil.go`, `runnable.go`) exposes the Linux/Android memfd implementation.
- `efrun/` mirrors the API for platforms without `memfd_create`; its tests live alongside the code.
- `adapters/` holds concrete runner and capture implementations; pair them with `port/` interfaces when adding new behavior.
- Tests sit next to the code (`*_test.go`) to keep scenarios close to their subjects; reusable fixtures belong under `adapters/mockrunner`.

## Build, Test, and Development Commands
- `go test ./...` runs the full suite across all packages; use it before every commit.
- `GOOS=linux GOARCH=amd64 go test ./...` verifies cross-platform stubs when developing on non-Linux hosts.
- `go vet ./...` catches common mistakes; integrate it into your editor or pre-push routine.
- `go build ./...` ensures packages compile; run it whenever public APIs change.

## Coding Style & Naming Conventions
- Rely on `go fmt ./...`; committed code should already match gofmt output (tabs for indentation, grouped imports).
- Keep exported symbols documented with Go-style comments (`// Name ...`) and prefer descriptive, action-oriented names.
- Align new adapters with existing naming (`commandrunner`, `commandcapture`) and reuse shared helpers from `port/` where possible.

## Testing Guidelines
- Favor table-driven tests named `Test<Thing>` for behavior and `Test<Thing>_<Case>` for edge cases; keep fixtures small and explicit.
- Use `context` deadlines in integration tests to avoid hanging processes.
- Track coverage locally with `go test -cover ./...`; target parity with existing packages when adding features.

## Commit & Pull Request Guidelines
- Follow the imperative, concise style in Git history (e.g., `Add WaitWithContext`). Keep summaries under ~60 characters when possible.
- Each PR should explain motivation, note behavioral impacts, and call out how to reproduce testing (`go test ./...`).
- Link related issues and include logs or screenshots if the change affects observable output.

## Security & Configuration Tips
- Audit embedded payloads or scripts before bundling; treat them like shipped binaries.
- When adding adapters that spawn processes, ensure arguments are validated to avoid shell injection.
- Use `emrun.WithPolicy` plus `emrun.WithRule` to enforce SHA-256 allow/deny lists for bundled executables; load hashes from `sha256sum` files or raw digests before invoking `Run*`/`Do*` helpers.
- Document any environment variables or kernel prerequisites in the PR so operators can mirror the setup.
