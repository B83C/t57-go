# t57-go

A robust, cross-platform CLI for the **T57 RFID reader/programmer**, written in Go.

This is a ground-up rewrite of the original Zig tool at `~/z/t57`. Compared to the
Zig version, this rewrite:

* **Doesn't crash on bad input** — every operation returns a typed error
  instead of panicking.
* **Retries transient errors** — BCC mismatches, short reads, and
  timeouts are automatically retried up to N times (default 3).
* **Saves recent reads** to a JSONL history file under
  `$XDG_DATA_HOME/t57/history.jsonl`, so you can replay them later.
* **Supports arbitrary commands** — `t57 raw --command 0x83 -d ""` lets
  you send any byte the firmware accepts.
* **Cross-platform** — builds for Linux, macOS, and Windows from the
  same source.
* **Self-contained** — no project-local vendor copies; the only
  third-party dep is `go.bug.st/serial`.
* **Tested** — table-driven unit tests for the frame protocol, config
  bit fields, hex parsing; integration tests for the high-level client
  against a deterministic mock transport.

## Build

### Native CLI

```sh
go build -o t57 ./cmd/t57
```

Cross-compile for another OS:

```sh
GOOS=linux   GOARCH=amd64 go build -o t57-linux   ./cmd/t57
GOOS=darwin  GOARCH=arm64 go build -o t57-darwin  ./cmd/t57
GOOS=windows GOARCH=amd64 go build -o t57.exe     ./cmd/t57
```

The binary is fully static (no CGo) and weighs in around 9 MB with the web UI embedded.

### WASM (Web Serial API)

The CLI can also serve a **web UI** that talks to the T57 via the browser's
`navigator.serial` API. Build:

```sh
GOOS=js GOARCH=wasm go build -ldflags="-s -w" -o web/dist/t57.wasm ./cmd/t57wasm/
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" web/dist/
```

Then serve it:

```sh
# Option 1: built-in server (recommended)
t57 serve
# → open http://localhost:8080 in Chrome/Edge

# Option 2: any static file server
cd web && python3 -m http.server 8080
# → open http://localhost:8080 in Chrome/Edge
```

**Browser support:** Desktop Chrome/Edge (Windows, Linux, macOS) and
Android Chrome. **iOS Safari is not supported** — the Web Serial API
is absent from WebKit. For iOS, use the native CLI over a serial-over-IP
bridge (ESP32 + ser2net + `t57 -p tcp://host:port`).

Read more about the web architecture in [`docs/web.md`](docs/web.md).

## Quick start

```sh
# Find the device (probes every available serial port).
t57 scan

# Read a single block.
t57 read -b 1
# block 1: 0xDEADBEEF

# Read all six user blocks.
t57 read

# Write a block.
t57 write -b 1 -w DEADBEEF
# about to write 0xDEADBEEF to block 1; continue? [y/N] y
# wrote 0xDEADBEEF to block 1

# Read the config block.
t57 read --config
# config: 0x000880E8 (Config{master_key=0x0, data_bit_rate=RF/32, ...})

# Send an arbitrary command (raw mode).
t57 raw --command 0x83
# 0xAABBCC0011223344

# Show recent history.
t57 history list
t57 history last
```

## Commands

| Command | What it does |
|---|---|
| `scan` | Probe every serial port for a T57 device. |
| `ports` | List serial ports on the host. |
| `read [-b N] [--config] [-s N -c N]` | Read blocks. `--config` reads the config block. `-s/-c` read a range. |
| `write -b N -w HEX,HEX,...` | Write blocks. Prompts for confirmation unless `-y`. |
| `raw --command 0xNN -d HEX` | Send a raw frame, print the response. |
| `duplicate` | Read a tag, then write the same data to another tag. |
| `config {read\|write HEX\|default}` | Read/write the config block (block 0). |
| `baud 9600\|19200\|38400\|57600\|115200` | Change device baud rate. |
| `address N` | Change device station address. |
| `buzzer --on N --cycles N` | Beep the buzzer. |
| `led --on N --cycles N` | Blink the LED. |
| `version` | Print firmware version string. |
| `history {list\|show N\|last\|clear\|path}` | Manage the history log. |
| `help` | Print usage. |

## Global flags

| Flag | Default | Description |
|---|---|---|
| `-p, --port` | `auto` | Serial port path. `auto` probes every available port. |
| `--baud` | `115200` | Default baud rate (used for auto-detect). |
| `--retries` | `3` | Retry count for transient errors. |
| `-y, --yes` | `false` | Skip confirmation prompts on writes. |
| `-v, --verbose` | `false` | Print extra info on stderr. |
| `--json` | `false` | Machine-readable JSON output. |
| `--no-history` | `false` | Don't append reads to the history log. |
| `--history PATH` | `$XDG_DATA_HOME/t57/history.jsonl` | Override the history file location. |

## Architecture

```
┌─────────────────────────────────────┐
│ cmd/t57          (CLI front-end)    │  arg parsing, history, JSON output
└────────────────┬────────────────────┘
                 │
┌────────────────▼────────────────────┐
│ internal/t57    (protocol layer)    │  frame codec, config struct, high-level Client
│   ├─ frame.go    encode/decode      │
│   ├─ command.go  command codes      │
│   ├─ config.go   32-bit config      │
│   ├─ client.go   high-level ops     │
│   ├─ history.go  JSONL log          │
│   └─ errors.go   typed errors       │
└────────────────┬────────────────────┘
                 │
┌────────────────▼────────────────────┐
│ internal/serial (transport layer)   │  go.bug.st/serial
│  or internal/mock  (testing)        │  deterministic in-memory
└─────────────────────────────────────┘
```

The protocol layer is **transport-agnostic** — you can swap in any
byte-stream source (USB, TCP, a fake device, ...). The CLI uses
`internal/serial`; tests use `internal/mock`.

## Robustness

Things the original Zig tool does that this one does *not* do:

* **Doesn't panic on bad input.** Hex strings are parsed; out-of-range
  block numbers return `ErrOutOfRange`; missing serial ports return
  `ErrPortNotFound`. Use `errors.Is(err, t57.ErrXxx)` to test for
  specific failure modes.
* **Retries transient errors.** A bad checksum, an empty read, or an
  I/O error causes the client to re-send the request up to `--retries`
  times. See `t57-core/client.go` for the list of "transient" errors.
* **Bounded reads.** Every read goes through a wrapper that returns a
  typed error after a short timeout, so the CLI never blocks forever
  on a dead device.
* **Auto-detect.** If you don't pass `--port`, the CLI probes every
  available serial port and uses the first one that responds to
  `SysGetSerlNum`.
* **History is crash-safe.** Each entry is appended with `O_APPEND` so
  an interrupted write can't corrupt the file. Malformed lines in an
  existing file are silently skipped.
* **Saves every read** to the history log so you can recover your
  work after a restart.

## Testing

```sh
go test ./...
go test -race -coverprofile=cover.out ./...
go tool cover -html cover.out
```

The test suite covers:

* `frame.go` — every reject path (bad STX/ETX, bad BCC, length
  mismatch, short frame, oversize payload, ...).
* `config.go` — every field accessor, both LE and BE byte orders,
  round-trips, out-of-range errors.
* `client.go` — every high-level method (`SerialNumber`, `ReadBlock`,
  `ReadConfig`, `WriteConfig`, ...), retry logic, device-error
  decoding, integration with the mock transport.
* `history.go` — add, reload from disk, clear, capacity cap.
* `mock.go` — read/write, EOF, closed-pipe, error injection.
* `cmd/t57-integration` — end-to-end CLI smoke tests.

## Known limitations

* **Baud-rate persistence:** the spec says the new baud rate only
  takes effect after a power-cycle. The CLI does not reconfigure the
  host's serial port automatically, so the next command after a `baud`
  change will fail until you reset the device.
* **Page-1 reads** are supported (`read --page 1 -b N`) but the spec
  is silent on what those blocks mean for a freshly-erased tag.
* **Only the T5557 command set is exposed** as typed methods. ISO
  14443 / ISO 15693 commands can still be sent via `raw --command`.
* **No built-in support for very long payloads** (>254 bytes). Use
  the on-wire `DATA` field directly via `raw`.

## License

MIT OR Apache-2.0, at your option.
