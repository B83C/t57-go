// Package mock provides a deterministic in-memory Transport for testing
// the t57 package without real hardware.
package mock

import (
	"errors"
	"io"
	"sync"
)

// Transport is a fake T57 device. It owns two byte queues: a
// host-to-device queue (`tx`) and a device-to-host queue (`rx`).
// Tests push responses onto `rx` and inspect the host's writes via
// `tx`.
type Transport struct {
	mu          sync.Mutex
	tx          []byte
	rx          []byte
	readCount   int
	writeCount  int
	closed      bool
	readErr     error // if non-nil, returned on next Read
	writeErr    error // if non-nil, returned on next WriteAll
	flushErr    error // if non-nil, returned on next Flush
	echo        bool   // if true, every byte written is appended to rx
}

// New returns a new empty mock transport.
func New() *Transport { return &Transport{} }

// PushResponse queues bytes for the next Read calls.
func (t *Transport) PushResponse(b []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rx = append(t.rx, b...)
}

// TxBytes returns the bytes the host has written so far. The slice
// aliases internal state; copy if you need to keep it.
func (t *Transport) TxBytes() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tx
}

// ClearTx empties the host's write queue. Useful between scripted
// commands.
func (t *Transport) ClearTx() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tx = t.tx[:0]
}

// SetReadError forces the next Read to fail with the given error.
func (t *Transport) SetReadError(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.readErr = err
}

// SetWriteError forces the next WriteAll to fail.
func (t *Transport) SetWriteError(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.writeErr = err
}

// SetEcho makes every byte written also appear in the read queue.
// Useful for loopback tests.
func (t *Transport) SetEcho(on bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.echo = on
}

// ReadCount returns the number of times Read has been called.
func (t *Transport) ReadCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.readCount
}

// WriteCount returns the number of times WriteAll has been called.
func (t *Transport) WriteCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.writeCount
}

// WriteAll implements t57.Transport.
func (t *Transport) WriteAll(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return 0, io.ErrClosedPipe
	}
	if t.writeErr != nil {
		err := t.writeErr
		t.writeErr = nil
		return 0, err
	}
	t.writeCount++
	t.tx = append(t.tx, p...)
	if t.echo {
		t.rx = append(t.rx, p...)
	}
	return len(p), nil
}

// Read implements t57.Transport.
func (t *Transport) Read(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return 0, io.ErrClosedPipe
	}
	if t.readErr != nil {
		err := t.readErr
		t.readErr = nil
		return 0, err
	}
	t.readCount++
	if len(t.rx) == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if n > len(t.rx) {
		n = len(t.rx)
	}
	copy(p, t.rx[:n])
	t.rx = t.rx[n:]
	return n, nil
}

// Drain implements t57.Transport.
func (t *Transport) Drain() error { return nil }

// Flush implements t57.Transport.
func (t *Transport) Flush() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return io.ErrClosedPipe
	}
	if t.flushErr != nil {
		err := t.flushErr
		t.flushErr = nil
		return err
	}
	return nil
}

// Close implements t57.Transport.
func (t *Transport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	return nil
}

// ErrTimeout is a fake timeout error for tests.
var ErrTimeout = errors.New("mock: timeout")
