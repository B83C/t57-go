# Web UI Architecture

The T57 web UI uses WebAssembly (WASM) to run the exact same Go protocol
code (`internal/t57`) inside the browser, bridged to the physical T57
reader via the browser's [Web Serial API](https://developer.mozilla.org/en-US/docs/Web/API/Web_Serial_API).

## Architecture

```
┌─────────────────────────────────────────────────┐
│                   Browser (Chrome/Edge)          │
│                                                   │
│  ┌─────────────────────────────────────────┐     │
│  │ index.html + app.js                     │     │
│  │   │                                      │     │
│  │   ├── calls t57Connect() → Go WASM      │     │
│  │   ├── calls t57ReadBlock() → Go WASM    │     │
│  │   └── calls t57WriteBlock() → Go WASM   │     │
│  └──────────────┬──────────────────────────┘     │
│                 │  js.FuncOf bridge              │
│  ┌──────────────▼──────────────────────────┐     │
│  │  t57.wasm (GOOS=js GOARCH=wasm)         │     │
│  │  ├── internal/t57 (protocol)             │     │
│  │  └── internal/serial/wasm.go (WebSerial) │     │
│  └──────────────┬──────────────────────────┘     │
│                 │  navigator.serial               │
│  ┌──────────────▼──────────────────────────┐     │
│  │  USB-C ↔ T57 RFID reader               │     │
│  └─────────────────────────────────────────┘     │
└─────────────────────────────────────────────────┘
```

## How it works

1. **`t57 serve`** starts a local HTTP server (`:8080`) serving an
   embedded HTML page, the WASM binary, and Go's `wasm_exec.js` runtime.

2. **`wasm_exec.js`** loads the Go WASM runtime and instantiates
   `t57.wasm` via `WebAssembly.instantiateStreaming`.

3. **`main.go` in `cmd/t57wasm/`** registers JavaScript-callable
   functions (`t57Connect`, `t57ReadBlock`, etc.) using `js.FuncOf`.

4. You click **Connect** → calls `t57Connect(115200)` → Go calls
   `navigator.serial.requestPort()` which shows the browser's serial
   port picker. User selects the T57 device.

5. The port is opened at 115200 baud. Go verifies the device by
   calling `SysGetSerlNum`. If successful, a `*t57.Client` is created
   and stored globally in the WASM module.

6. All subsequent operations (`ReadBlock`, `WriteBlock`, etc.) use
   this client. The Go code calls the Web Serial API via `syscall/js`
   Promises, handshake with an `await` helper that blocks Go on a
   channel and yields to the JS event loop.

## File layout

```
t57-go/
├── cmd/
│   ├── t57/                 # native CLI + embedded web assets
│   │   ├── main.go
│   │   └── web/             # embed.FS copy of web assets
│   └── t57wasm/             # WASM entry point (js + wasm build tag)
│       └── main.go
├── internal/
│   ├── t57/                 # protocol core (shared by CLI + WASM)
│   └── serial/
│       ├── serial.go        # native serial (build tag: !js)
│       └── wasm.go          # Web Serial bridge (build tag: js)
└── web/
    ├── index.html           # source of truth for the HTML UI
    └── dist/                # built WASM + wasm_exec.js
```

## Async model

Go WASM is single-threaded (no OS threads). When Go code blocks on a
channel (e.g. waiting for a JS Promise), the WASM runtime yields to
the browser's event loop, which can process serial data, user input,
and Promise resolutions. When the Promise settles, the Go callback
fires and unblocks the waiting goroutine.

This is handled by the `await()` helper in `internal/serial/wasm.go`:

```
Go  ──► promise.then(onFulfilled).catch(onRejected)
         │
         ├── blocks on channel
         ├── JS event loop runs
         │   └── Promise resolves → callback fires → channel unblocks
         └── returns result
```

All exported JS functions are synchronous from JS's perspective (no
`await` needed in the JS caller), but inside Go they may pause briefly
while JS events are processed.

## Platform support

| Platform | Web Serial | Works |
|---|---|---|
| Chrome/Edge (desktop) | ✅ | Yes |
| Android Chrome | ✅ | Yes |
| iOS Safari | ❌ | No (use native CLI + network bridge) |
| iOS Chrome/Firefox | ❌ | No (WebKit limitation) |

For iOS, the only realistic path is a serial-over-IP bridge (Raspberry
Pi, ESP32, etc.) running `ser2net`, combined with a WebSocket proxy.
The web UI would need a `t57 serve` running on the bridge machine.
