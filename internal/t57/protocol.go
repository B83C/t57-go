// Package t57 implements the wire protocol and high-level client for the
// T57 RFID reader/programmer.
//
// The package is structured in layers:
//
//   - frame.go   — pure encode/decode of wire frames. No I/O.
//   - command.go — command code, status code, and baud code enums.
//   - config.go  — the 32-bit configuration block.
//   - transport.go — the Transport interface (a serial port, a mock for
//     tests, anything).
//   - client.go  — the high-level API built on top of Transport.
//   - history.go — JSONL history of recent reads/writes.
//   - errors.go  — typed errors returned by the package.
//
// Every fallible call returns a typed error. Nothing panics on bad input.
package t57

// STX marks the first byte of every frame.
const STX byte = 0xAA

// ETX marks the last byte of every frame.
const ETX byte = 0xBB

// DefaultStation is the broadcast station address. Per the device
// protocol §1.2, the reader responds to any frame addressed to 0x00
// without performing an address match.
const DefaultStation byte = 0x00

// MaxBlock is the highest T5557 block address (page 0).
//
// Block 0 is the config block. Blocks 1..=MaxBlock are user data. Per
// §7.1.1 of the device spec, page 0 holds 8 blocks (0..=7).
const MaxBlock byte = 0x07

// BlockSize is the number of bytes in a single user data block.
const BlockSize = 4

// ConfigSize is the number of bytes in the config block.
const ConfigSize = 4

// SerialSize is the number of bytes in the device serial number.
const SerialSize = 8

// MaxData is the largest payload (data section) a single frame can carry.
// The on-wire length byte is a u8 and includes the command/status byte,
// so the maximum data payload is 255 - 1.
const MaxData = 254

// MinFrame is the smallest legal response frame: AA station len status bcc BB.
const MinFrame = 6

// MaxFrame is a generous upper bound on frame size, used for scratch
// buffers. A response carrying 254 bytes of payload is 5 + 254 = 259
// bytes; we round up to 300 to leave slack.
const MaxFrame = 300
