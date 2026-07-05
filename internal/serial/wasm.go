//go:build js

package serial

import (
	"fmt"
	"io"
	"syscall/js"
	"time"
)

// await blocks until the given JS Promise resolves, rejects, or
// `timeout` elapses. If timeout is <= 0, it waits forever.
func awaitTimeout(promise js.Value, timeout time.Duration) (js.Value, error) {
	done := make(chan js.Value, 1)
	fail := make(chan js.Value, 1)

	onFulfilled := js.FuncOf(func(this js.Value, args []js.Value) any {
		done <- args[0]
		return nil
	})
	onRejected := js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 {
			fail <- args[0]
		} else {
			fail <- js.Value{}
		}
		return nil
	})
	defer onFulfilled.Release()
	defer onRejected.Release()

	promise.Call("then", onFulfilled).Call("catch", onRejected)

	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		select {
		case val := <-done:
			return val, nil
		case errVal := <-fail:
			if !errVal.Truthy() {
				return js.Value{}, fmt.Errorf("promise rejected")
			}
			return js.Value{}, fmt.Errorf("%s", errVal.String())
		case <-t.C:
			return js.Value{}, fmt.Errorf("timeout after %v", timeout)
		}
	} else {
		select {
		case val := <-done:
			return val, nil
		case errVal := <-fail:
			if !errVal.Truthy() {
				return js.Value{}, fmt.Errorf("promise rejected")
			}
			return js.Value{}, fmt.Errorf("%s", errVal.String())
		}
	}
}

// await calls awaitTimeout with no timeout (waits forever).
func await(promise js.Value) (js.Value, error) {
	return awaitTimeout(promise, 0)
}

// Transport implements t57.Transport using the Web Serial API.
//
// The reader and writer are created once when the port opens and kept
// for the life of the session — recreating them on every call loses
// stream state and causes "could not find T57" errors.
type Transport struct {
	port         js.Value
	streamReader js.Value // ReadableStreamDefaultReader (kept open)
	streamWriter js.Value // WritableStreamDefaultWriter (kept open)
}

// CheckAvailable returns an error if navigator.serial is not available.
func CheckAvailable() error {
	if !js.Global().Get("navigator").Get("serial").Truthy() {
		return fmt.Errorf("Web Serial API not available (try Chrome/Edge/Android)")
	}
	return nil
}

// RequestPort calls navigator.serial.requestPort(). Users see the
// browser's serial-port picker dialog.
func RequestPort() (js.Value, error) {
	if err := CheckAvailable(); err != nil {
		return js.Value{}, err
	}
	promise := js.Global().Get("navigator").Get("serial").Call("requestPort")
	return awaitTimeout(promise, 60*time.Second)
}

// GetPorts returns the list of previously-authorized serial ports.
func GetPorts() ([]string, error) {
	if err := CheckAvailable(); err != nil {
		return nil, err
	}
	promise := js.Global().Get("navigator").Get("serial").Call("getPorts")
	val, err := await(promise)
	if err != nil {
		return nil, err
	}
	length := val.Get("length").Int()
	out := make([]string, 0, length)
	for i := 0; i < length; i++ {
		info := val.Index(i).Call("getInfo")
		vid := info.Get("usbVendorId").Int()
		pid := info.Get("usbProductId").Int()
		out = append(out, fmt.Sprintf("%04X:%04X", vid, pid))
	}
	return out, nil
}

// Open opens the serial port at `baud` baud and returns a Transport.
func Open(port js.Value, baud int) (*Transport, error) {
	if !port.Truthy() {
		return nil, fmt.Errorf("no port selected")
	}
	opts := map[string]interface{}{
		"baudRate": baud,
		"dataBits": 8,
		"stopBits": 1,
		"parity":   "none",
	}
	consoleLog("debug", "t57: calling port.open(%v)", opts)

	openPromise := port.Call("open", opts)
	result, err := awaitTimeout(openPromise, 8*time.Second)
	if err != nil {
		consoleLog("error", "t57: port.open rejected: %v", err)
		return nil, fmt.Errorf("open port at %d baud: %v", baud, err)
	}
	consoleLog("debug", "t57: port.open resolved ok, result=%v", result)

	readable := port.Get("readable")
	if !readable.Truthy() {
		consoleLog("error", "t57: readable stream not available")
		_ = closePort(port)
		return nil, fmt.Errorf("readable stream not available")
	}
	writable := port.Get("writable")
	if !writable.Truthy() {
		consoleLog("error", "t57: writable stream not available")
		_ = closePort(port)
		return nil, fmt.Errorf("writable stream not available")
	}

	reader := readable.Call("getReader")
	writer := writable.Call("getWriter")
	consoleLog("debug", "t57: reader and writer acquired")

	return &Transport{
		port:         port,
		streamReader: reader,
		streamWriter: writer,
	}, nil
}

// consoleLog calls console.debug or console.error in the browser.
func consoleLog(level string, format string, args ...interface{}) {
	console := js.Global().Get("console")
	if !console.Truthy() {
		return
	}
	msg := fmt.Sprintf(format, args...)
	console.Call(level, msg)
}

// closePort closes a SerialPort and returns any error.
func closePort(port js.Value) error {
	closePromise := port.Call("close")
	_, err := awaitTimeout(closePromise, 3*time.Second)
	return err
}

// WriteAll implements t57.Transport.
func (t *Transport) WriteAll(p []byte) (int, error) {
	if !t.streamWriter.Truthy() {
		return 0, fmt.Errorf("not connected")
	}
	jsBuf := js.Global().Get("Uint8Array").New(len(p))
	js.CopyBytesToJS(jsBuf, p)

	writePromise := t.streamWriter.Call("write", jsBuf)
	if _, err := await(writePromise); err != nil {
		return 0, fmt.Errorf("write: %v", err)
	}
	return len(p), nil
}

// Read implements t57.Transport.
func (t *Transport) Read(p []byte) (int, error) {
	if !t.streamReader.Truthy() {
		return 0, io.EOF
	}

	readPromise := t.streamReader.Call("read")
	result, err := await(readPromise)
	if err != nil {
		return 0, fmt.Errorf("read: %v", err)
	}
	if result.Get("done").Bool() {
		return 0, io.EOF
	}
	value := result.Get("value")
	if !value.Truthy() {
		// Empty chunk — ask again, same as a zero-byte buffer.
		return 0, nil
	}
	n := value.Get("length").Int()
	if n > len(p) {
		n = len(p)
	}
	jsBuf := js.Global().Get("Uint8Array").New(value)
	goBuf := make([]byte, n)
	js.CopyBytesToGo(goBuf, jsBuf)
	copy(p, goBuf)
	return n, nil
}

// Drain implements t57.Transport — no-op for Web Serial (the stream
// reader keeps state, so we can't just read-and-discard).
func (t *Transport) Drain() error { return nil }

// Flush implements t57.Transport — no-op for Web Serial.
func (t *Transport) Flush() error { return nil }

// Close implements t57.Transport.
func (t *Transport) Close() error {
	if !t.port.Truthy() {
		return nil
	}
	if t.streamReader.Truthy() {
		t.streamReader.Call("cancel")
		t.streamReader = js.Value{}
	}
	if t.streamWriter.Truthy() {
		t.streamWriter.Call("close")
		t.streamWriter = js.Value{}
	}
	return closePort(t.port)
}
