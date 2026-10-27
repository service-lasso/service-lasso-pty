# service-lasso-pty

Pseudo-terminal host for Service Lasso managed services.

`service-lasso-pty` starts one managed process on a real PTY and bridges it to
the owning runtime over newline-delimited JSON frames on stdin/stdout. It is
the process Core spawns when a service opts into terminal attachment, so the
service's full-screen TUI can be rendered by a terminal emulator instead of a
line-oriented log view.

This is not a shell: the helper starts exactly one managed process, inherits the
caller's environment and working directory rules, and never accepts a second
command.

## Contract

```text
service-lasso-pty [--rows N] [--cols N] [--cwd DIR] -- COMMAND [ARG...]
```

- `--rows`/`--cols` (1-1000, default 24x80) set the initial window size.
- `--cwd` sets the managed process working directory.
- The managed process inherits the helper's environment.
- Parent stdin EOF **terminates** the managed process. The helper never outlives
  the runtime that owns it.

### Frames (helper to parent)

One JSON object per line, UTF-8, at most 1 MiB per line:

```json
{"t":"ready","pid":1234}
{"t":"data","b":"<base64 terminal bytes>"}
{"t":"exit","code":0}
{"t":"error","message":"..."}
```

- `ready` is emitted once, after the process is attached.
- `data` carries raw terminal bytes in order (output chunks are at most 32 KiB
  before base64 encoding).
- `exit` is emitted once, after the process is reaped. `code` is the managed
  exit status.
- `error` is emitted when the helper cannot start the process.

### Frames (parent to helper)

```json
{"t":"input","b":"<base64 bytes>"}
{"t":"resize","rows":40,"cols":120}
{"t":"kill"}
```

- `input` writes raw bytes to the PTY.
- `resize` changes the PTY window size.
- `kill` terminates the managed process.
- Malformed, empty, unknown, or oversized frames terminate the managed process;
  the helper fails closed.

## Security and limits

- The helper performs no authentication or authorization. The owning runtime
  must authorize every attach session before writing frames.
- PTY output merges stdout and stderr and can contain secrets; redaction is the
  caller's responsibility. The helper never persists or replays a transcript.
- One managed process per helper. There is no shell, no command chaining, no
  additional session, and no reattach after the runtime disconnects.
- Bounded framing (1 MiB line, 32 KiB chunks) prevents unbounded buffering.

## Platforms

| Platform | PTY implementation |
| --- | --- |
| Windows 10 1809+ | ConPTY (`CreatePseudoConsole`) |
| Linux | `openpty` |
| macOS | `openpty` |

The PTY layer is [`github.com/aymanbagabas/go-pty`](https://github.com/aymanbagabas/go-pty)
(MIT), pinned in `go.mod`.

## Build

```sh
go build ./...
go build -o service-lasso-pty ./cmd/service-lasso-pty
```

Cross-compilation (the Core release staging path builds all targets from one
Linux runner):

```sh
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/service-lasso-pty
GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/service-lasso-pty
GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/service-lasso-pty
GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/service-lasso-pty
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/service-lasso-pty
```

## Test

```sh
go vet ./...
go test ./... -count=1
```

Session tests spawn real processes on a real PTY: output streaming, bounded
input handling, and termination when the parent disconnects.

## Status

Phase 1 helper (spawn, stream, input, resize, exit, ownership). Core integration
(manifest capability, supervisor wiring, session API, Admin terminal emulator)
is tracked separately in the Core repository.
