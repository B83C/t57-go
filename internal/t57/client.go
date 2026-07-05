package t57

import (
	"errors"
	"io"
	"time"
)

// Client is the high-level API on top of a Transport.
//
// Every method returns a typed error. Nothing panics on bad input.
type Client struct {
	transport  Transport
	station    byte
	maxRetries int
	scratch    [MaxFrame]byte
	scratchLen int
	lastStatus byte

	// Per-call read timeout. Zero means no timeout (block forever).
	readTimeout time.Duration
	// Optional deadline for the next call. Mainly for tests.
	nextDeadline time.Time
}

// NewClient wraps a Transport with a high-level client.
func NewClient(t Transport) *Client {
	return &Client{
		transport:  t,
		station:    DefaultStation,
		maxRetries: 3,
	}
}

// WithStation returns the client with a different station address.
func (c *Client) WithStation(addr byte) *Client {
	c.station = addr
	return c
}

// WithRetries returns the client with a different retry count.
func (c *Client) WithRetries(n int) *Client {
	c.maxRetries = n
	return c
}

// WithReadTimeout returns the client with a per-call read timeout. The
// timeout is enforced by the underlying serial transport; the client
// itself does not busy-loop.
func (c *Client) WithReadTimeout(d time.Duration) *Client {
	c.readTimeout = d
	return c
}

// Transport returns the underlying transport.
func (c *Client) Transport() Transport { return c.transport }

// LastStatus returns the status byte from the most recent successful
// transaction.
func (c *Client) LastStatus() byte { return c.lastStatus }

// SetDeadline sets an absolute deadline for the next call (used by
// the mock transport to inject latency / timeouts in tests).
func (c *Client) SetDeadline(t time.Time) { c.nextDeadline = t }

// Transact sends a command and returns the response payload.
//
// The returned slice borrows from the client's scratch buffer and is
// invalidated by the next call. Higher-level methods (`ReadBlock`,
// `ReadConfig`, ...) copy the response into an owned value, so they
// are safe to keep around.
func (c *Client) Transact(cmd Command, data []byte) ([]byte, error) {
	if len(data) > MaxData {
		return nil, makeErr("transact", "payload_too_large", ErrPayloadTooLarge,
			map[string]any{"len": len(data), "max": MaxData})
	}
	var lastErr *Error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := c.transactOnce(cmd, data); err != nil {
			var te *Error
			if errors.As(err, &te) && te.IsTransient() && attempt < c.maxRetries {
				lastErr = te
				continue
			}
			return nil, err
		}
		return c.scratch[:c.scratchLen], nil
	}
	return nil, lastErr
}

func (c *Client) transactOnce(cmd Command, data []byte) error {
	var tx [MaxData + 5]byte
	enc, err := Encode(tx[:], c.station, cmd, data)
	if err != nil {
		return err
	}
	if _, err := c.transport.WriteAll(tx[:enc.Len]); err != nil {
		return makeErr("write", "io", err, nil)
	}
	if err := c.transport.Flush(); err != nil {
		return makeErr("flush", "io", err, nil)
	}

	// Read a full frame (until ETX or buffer full).
	n, err := ReadFrame(c.transport, c.scratch[:])
	if err != nil {
		return makeErr("read_frame", "io", err, nil)
	}
	if n == 0 {
		return makeErr("read", "frame_too_short", ErrFrameTooShort,
			map[string]any{"received": 0, "minimum": MinFrame})
	}
	dec, err := Decode(c.scratch[:n])
	if err != nil {
		return err
	}
	c.lastStatus = dec.Status
	if dec.Status != 0x00 {
		sub := byte(0)
		if len(dec.Data) > 0 {
			sub = dec.Data[0]
		}
		return makeErr("status", "device_error", ErrDeviceError,
			map[string]any{"status": dec.Status, "device_error": ParseDeviceError(sub)})
	}
	// Copy the data portion to the front of the scratch so callers
	// can read `scratch[:dataLen]` and get just the payload.
	copy(c.scratch[:], dec.Data)
	c.scratchLen = len(dec.Data)
	return nil
}

// --- typed high-level methods ---

// ReadBlock reads a T5557 user data block by 1-based index (1..=MaxBlock).
func (c *Client) ReadBlock(block byte) ([BlockSize]byte, error) {
	if block < 1 || block > MaxBlock {
		return [BlockSize]byte{}, makeErr("read_block", "out_of_range", ErrOutOfRange,
			map[string]any{"field": "block", "value": block, "min": 1, "max": MaxBlock})
	}
	return c.ReadBlockRaw(Page0, block)
}

// ReadBlockRaw reads any T5557 block (0..=MaxBlock) at the given page.
func (c *Client) ReadBlockRaw(page Page, block byte) ([BlockSize]byte, error) {
	if block > MaxBlock {
		return [BlockSize]byte{}, makeErr("read_block_raw", "out_of_range", ErrOutOfRange,
			map[string]any{"field": "block", "value": block, "min": 0, "max": MaxBlock})
	}
	raw, err := c.Transact(CmdT5557Read, []byte{byte(page), block})
	if err != nil {
		return [BlockSize]byte{}, err
	}
	if len(raw) < BlockSize {
		return [BlockSize]byte{}, makeErr("read_block_raw", "buffer_too_small", ErrBufferTooSmall,
			map[string]any{"needed": BlockSize, "got": len(raw)})
	}
	var out [BlockSize]byte
	// Some devices ignore the block address and always return a
	// cascade dump [block1, block2, ...] — extract the requested
	// block by position.
	if len(raw) > BlockSize && block >= 1 {
		pos := int(block-1) * BlockSize
		if pos+BlockSize <= len(raw) {
			copy(out[:], raw[pos:pos+BlockSize])
			return out, nil
		}
	}
	copy(out[:], raw[:BlockSize])
	return out, nil
}

// ReadBlocks reads `count` consecutive user blocks starting at `start`.
// count must be in 1..=MaxBlock.
func (c *Client) ReadBlocks(start, count byte) ([][BlockSize]byte, error) {
	if count == 0 || int(count) > int(MaxBlock) {
		return nil, makeErr("read_blocks", "out_of_range", ErrOutOfRange,
			map[string]any{"field": "count", "value": count, "min": 1, "max": MaxBlock})
	}
	if start == 0 || int(start)+int(count)-1 > int(MaxBlock) {
		return nil, makeErr("read_blocks", "out_of_range", ErrOutOfRange,
			map[string]any{"field": "start", "value": start, "min": 1, "max": MaxBlock})
	}
	// If reading all or most blocks, use the fast cascade path.
	if start == 1 && count >= 6 {
		all, err := c.ReadAllRaw()
		if err == nil {
			out := make([][BlockSize]byte, count)
			for i := byte(0); i < count; i++ {
				out[i] = all[start+i]
			}
			return out, nil
		}
		// Fall through to individual reads.
	}
	out := make([][BlockSize]byte, count)
	for i := byte(0); i < count; i++ {
		b, err := c.ReadBlock(start + i)
		if err != nil {
			return nil, err
		}
		out[i] = b
	}
	return out, nil
}

// WriteBlock writes 4 bytes to a T5557 block (0..=MaxBlock). Writes are
// only valid on page 0 per the spec.
func (c *Client) WriteBlock(block byte, data [BlockSize]byte) error {
	if block > MaxBlock {
		return makeErr("write_block", "out_of_range", ErrOutOfRange,
			map[string]any{"field": "block", "value": block, "min": 0, "max": MaxBlock})
	}
	payload := make([]byte, 2+BlockSize)
	payload[0] = byte(Page0)
	payload[1] = block
	copy(payload[2:], data[:])
	_, err := c.Transact(CmdT5557Write, payload)
	return err
}

// ReadConfig reads the 4-byte config block (block 0, page 0).
func (c *Client) ReadConfig() (Config, error) {
	raw, err := c.ReadBlockRaw(Page0, 0)
	if err != nil {
		return Config{}, err
	}
	return ConfigFromLEBytes(raw), nil
}

// ReadAllRaw reads all blocks in one shot by reading block 1
// (which triggers a cascade dump on many cards), then reading
// the config block separately.
func (c *Client) ReadAllRaw() ([8][4]byte, error) {
	var out [8][4]byte
	scratch, err := c.Transact(CmdT5557Read, []byte{byte(Page0), 1})
	if err != nil {
		return out, err
	}
	// Response may contain 1..=7 user blocks starting from block 1.
	n := len(scratch) / BlockSize
	if n > 7 {
		n = 7
	}
	for i := 0; i < n; i++ {
		copy(out[i+1][:], scratch[i*4:i*4+4])
	}
	// Any user blocks not covered by the cascade.
	for bi := n + 1; bi <= 7; bi++ {
		b, err := c.ReadBlock(uint8(bi))
		if err != nil {
			return out, err
		}
		out[bi] = b
	}
	// Config block separately.
	cfg, err := c.ReadConfig()
	if err != nil {
		return out, err
	}
	out[0] = cfg.LEBytes()
	return out, nil
}

// WriteConfig writes a Config to block 0. The 4 config bytes are sent
// in little-endian order, matching the host's byte order on common
// workstations.
func (c *Client) WriteConfig(cfg Config) error {
	raw := cfg.LEBytes()
	return c.WriteBlock(0, raw)
}

// SerialNumber reads the device's 8-byte serial number.
func (c *Client) SerialNumber() ([SerialSize]byte, error) {
	raw, err := c.Transact(CmdSysGetSerlNum, nil)
	if err != nil {
		return [SerialSize]byte{}, err
	}
	if len(raw) < SerialSize {
		return [SerialSize]byte{}, makeErr("serial_number", "buffer_too_small", ErrBufferTooSmall,
			map[string]any{"needed": SerialSize, "got": len(raw)})
	}
	var out [SerialSize]byte
	copy(out[:], raw[:SerialSize])
	return out, nil
}

// FirmwareVersion reads the firmware version string.
func (c *Client) FirmwareVersion() ([]byte, error) {
	return c.Transact(CmdSysGetVersion, nil)
}

// SetBaud requests a change of the device's serial baud rate. Per
// §3.1.2 of the spec, the new rate only takes effect after a
// power-cycle or reset.
func (c *Client) SetBaud(rate BaudCode) error {
	raw, err := c.Transact(CmdSysSetBaudrate, []byte{byte(rate)})
	if err != nil {
		return err
	}
	if len(raw) < 1 || raw[0] != byte(rate) {
		return makeErr("set_baud", "device_error", ErrDeviceError,
			map[string]any{"expected": byte(rate), "got": raw})
	}
	return nil
}

// SetAddress changes the device's station address.
func (c *Client) SetAddress(addr byte) error {
	raw, err := c.Transact(CmdSysSetAddress, []byte{addr})
	if err != nil {
		return err
	}
	if len(raw) < 1 || raw[0] != addr {
		return makeErr("set_address", "device_error", ErrDeviceError,
			map[string]any{"expected": addr, "got": raw})
	}
	return nil
}

// Buzzer beeps the buzzer. `on` is on-time in 20ms units; `cycles` is
// the number of on/off cycles.
func (c *Client) Buzzer(on, cycles byte) error {
	_, err := c.Transact(CmdSysControlBuzzer, []byte{on, cycles})
	return err
}

// LED blinks the LED. `on` is on-time in 20ms units; `cycles` is the
// number of on/off cycles.
func (c *Client) LED(on, cycles byte) error {
	_, err := c.Transact(CmdSysControlLed, []byte{on, cycles})
	return err
}

// Close releases the transport.
func (c *Client) Close() error {
	if c.transport == nil {
		return nil
	}
	return c.transport.Close()
}

// Sentinel for tests.
var _ = io.EOF
