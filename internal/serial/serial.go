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
}

// Open opens a serial port at the given path and baud rate with the
// device's default framing: 8N1, no flow control.
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

// Read implements t57.Transport.
func (t *Transport) Read(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n, err := t.port.Read(p)
	if err != nil {
		// goSerial returns an error on timeout but may have partial data.
		if n > 0 {
			return n, nil
		}
		// Map platform-specific timeouts to a stable sentinel.
		if isTimeout(err) {
			return 0, io.EOF
		}
		return n, err
	}
	return n, nil
}

// Flush implements t57.Transport.
func (t *Transport) Flush() error {
	// goSerial's Port has no explicit Flush; writes are immediate.
	return nil
}

// Drain implements t57.Transport.  Temporarily sets the read timeout
// to 1ms and reads whatever is available, discarding it.
func (t *Transport) Drain() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.port == nil {
		return nil
	}
	// Save the current timeout and temporarily set it very short.
	// We can't get the old timeout, so we just set 1ms and restore
	// the default 500ms.  This is lossy but adequate for draining.
	_ = t.port.SetReadTimeout(1 * time.Millisecond)
	buf := make([]byte, 256)
	for i := 0; i < 10; i++ {
		_, err := t.port.Read(buf)
		if err != nil {
			break
		}
	}
	return t.port.SetReadTimeout(DefaultReadTimeout)
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
