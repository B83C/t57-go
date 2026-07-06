// Package serial provides a serial-port Transport for t57-core.
//
// Build constraint: this file is used for native builds (not WASM).
// The WASM implementation lives in wasm.go.
//
//go:build !js
package serial

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	goSerial "go.bug.st/serial"
)

// DefaultBaud is the factory-default baud rate per §1.1 of the T57
// protocol spec.
const DefaultBaud = 115200

// DefaultReadTimeout is the default per-call read timeout.
const DefaultReadTimeout = 500 * time.Millisecond

// PortInfo describes one serial port the host can see.
type PortInfo struct {
	Name string
	Kind string
}

// ListPorts enumerates the serial ports the host can see. On Linux this
// is the contents of /dev/tty*; on macOS /dev/cu.*; on Windows the COM
// ports. Returns an empty list if the platform doesn't support
// enumeration.
func ListPorts() ([]PortInfo, error) {
	names, err := goSerial.GetPortsList()
	if err != nil {
		return nil, err
	}
	out := make([]PortInfo, 0, len(names))
	for _, n := range names {
		out = append(out, PortInfo{Name: n, Kind: "unknown"})
	}
	return out, nil
}

// Transport is a serial-port-backed t57.Transport.
type Transport struct {
	mu   sync.Mutex
	port goSerial.Port
	buf  []byte // internal buffer for over-read bytes
}

// Open opens a serial port at the given path and baud rate with the
// device's default framing: 8N1, no flow control.
//
// On Linux, we explicitly clear DTR and RTS after opening because
// many USB-serial chips assert these signals by default, which can
// reset or mute the connected module's transmitter.
func Open(path string, baud int) (*Transport, error) {
	return OpenWithTimeout(path, baud, DefaultReadTimeout)
}

// OpenWithTimeout opens a serial port with an explicit read timeout.
func OpenWithTimeout(path string, baud int, readTimeout time.Duration) (*Transport, error) {
	cfg := goSerial.Mode{
		BaudRate: baud,
		DataBits: 8,
		Parity:   goSerial.NoParity,
		StopBits: goSerial.OneStopBit,
	}
	port, err := goSerial.Open(path, &cfg)
	if err != nil {
		return nil, fmt.Errorf("open serial %s: %w", path, err)
	}
	// Some chips assert DTR/RTS on open, which can mute the module's
	// transmitter.  Clear these signals explicitly.
	if err := port.SetDTR(false); err != nil {
		port.Close()
		return nil, fmt.Errorf("set DTR: %w", err)
	}
	if err := port.SetRTS(false); err != nil {
		port.Close()
		return nil, fmt.Errorf("set RTS: %w", err)
	}
	if err := port.SetReadTimeout(readTimeout); err != nil {
		port.Close()
		return nil, fmt.Errorf("set read timeout: %w", err)
	}
	return &Transport{port: port}, nil
}

// Port returns the underlying serial port (escape hatch).
func (t *Transport) Port() goSerial.Port {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.port
}

// WriteAll implements t57.Transport.
func (t *Transport) WriteAll(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	written := 0
	for written < len(p) {
		n, err := t.port.Write(p[written:])
		if n > 0 {
			written += n
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return written, err
		}
	}
	return written, nil
}

// Read implements t57.Transport.  Returns buffered bytes first if any,
// then reads from the serial port.
func (t *Transport) Read(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Serve from the internal buffer first.
	if len(t.buf) > 0 {
		n := copy(p, t.buf)
		t.buf = t.buf[n:]
		return n, nil
	}

	n, err := t.port.Read(p)
	if err != nil {
		if n > 0 {
			// Partial data — stash overflow.
			if n < len(p) {
				return n, nil
			}
			return n, nil
		}
		if isTimeout(err) {
			return 0, io.EOF
		}
		return n, err
	}
	return n, nil
}

// ReadBuffered reads from the port into an internal buffer and returns
// the first `want` bytes.  Excess bytes are kept for the next call.
// This lets ReadFrame read in chunks without losing bytes past ETX.
func (t *Transport) ReadBuffered(want int) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// First serve from the existing buffer.
	if len(t.buf) >= want {
		out := t.buf[:want]
		t.buf = t.buf[want:]
		return out, nil
	}

	// Need more bytes — read a chunk from the port.
	chunk := make([]byte, 256)
	n, err := t.port.Read(chunk)
	if err != nil {
		if n == 0 {
			if isTimeout(err) && len(t.buf) > 0 {
				// Return what we have.
				out := t.buf
				t.buf = nil
				return out, nil
			}
			return nil, err
		}
	}
	t.buf = append(t.buf, chunk[:n]...)
	if len(t.buf) >= want {
		out := t.buf[:want]
		t.buf = t.buf[want:]
		return out, nil
	}
	// Not enough bytes yet — return what we have.
	out := t.buf
	t.buf = nil
	return out, nil
}

// Flush implements t57.Transport.
func (t *Transport) Flush() error {
	// goSerial's Port has no explicit Flush; writes are immediate.
	return nil
}

// Drain implements t57.Transport.  Reads and discards any bytes that
// have already been buffered, with a short timeout.
func (t *Transport) Drain() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.port == nil {
		return nil
	}
	// Save timeout and set very short for a single non-blocking read.
	old := DefaultReadTimeout
	_ = t.port.SetReadTimeout(1 * time.Millisecond)
	buf := make([]byte, 64)
	_, err := t.port.Read(buf)
	if err != nil {
		// Nothing to drain — expected.
	}
	return t.port.SetReadTimeout(old)
}

// Close implements t57.Transport.
func (t *Transport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.port == nil {
		return nil
	}
	return t.port.Close()
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(strings.ToLower(msg), "timeout")
}
