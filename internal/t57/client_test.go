package t57_test

import (
	"errors"
	"testing"

	"github.com/t57/t57-go/internal/mock"
	"github.com/t57/t57-go/internal/t57"
)

// helper: build a "AA station length status data... bcc BB" frame
// matching a known response.
func buildResponse(station byte, status byte, data ...byte) []byte {
	body := append([]byte{station, byte(1 + len(data)), status}, data...)
	bcc := byte(0)
	for _, b := range body {
		bcc ^= b
	}
	return append(append([]byte{t57.STX}, body...), bcc, t57.ETX)
}

func TestClientSerialNumber(t *testing.T) {
	m := mock.New()
	want := [t57.SerialSize]byte{1, 2, 3, 4, 5, 6, 7, 8}
	m.PushResponse(buildResponse(0, 0, want[:]...))

	c := t57.NewClient(m)
	got, err := c.SerialNumber()
	if err != nil {
		t.Fatalf("SerialNumber: %v", err)
	}
	if got != want {
		t.Fatalf("got % X, want % X", got, want)
	}
}

func TestClientSerialNumberBufferTooSmall(t *testing.T) {
	m := mock.New()
	// Build a response with only 4 data bytes (need 8).
	m.PushResponse(buildResponse(0, 0, 1, 2, 3, 4))

	c := t57.NewClient(m)
	_, err := c.SerialNumber()
	if !errors.Is(err, t57.ErrBufferTooSmall) {
		t.Fatalf("err = %v, want ErrBufferTooSmall", err)
	}
}

func TestClientReadBlock(t *testing.T) {
	m := mock.New()
	want := [t57.BlockSize]byte{0xDE, 0xAD, 0xBE, 0xEF}
	m.PushResponse(buildResponse(0, 0, want[:]...))

	c := t57.NewClient(m).WithRetries(0)
	got, err := c.ReadBlock(1)
	if err != nil {
		t.Fatalf("ReadBlock: %v", err)
	}
	if got != want {
		t.Fatalf("got % X, want % X", got, want)
	}
}

func TestClientReadBlockOutOfRange(t *testing.T) {
	m := mock.New()
	c := t57.NewClient(m).WithRetries(0)
	_, err := c.ReadBlock(0)
	if !errors.Is(err, t57.ErrOutOfRange) {
		t.Fatalf("err = %v, want ErrOutOfRange", err)
	}
	_, err = c.ReadBlock(t57.MaxBlock + 1)
	if !errors.Is(err, t57.ErrOutOfRange) {
		t.Fatalf("err = %v, want ErrOutOfRange", err)
	}
}

func TestClientReadBlockDeviceError(t *testing.T) {
	m := mock.New()
	// Status 0x01, sub-code 0x89 = bad parameter.
	m.PushResponse(buildResponse(0, 0x01, 0x89))

	c := t57.NewClient(m).WithRetries(0)
	_, err := c.ReadBlock(1)
	if !errors.Is(err, t57.ErrDeviceError) {
		t.Fatalf("err = %v, want ErrDeviceError", err)
	}
	devErr, ok := t57.AsDeviceError(err)
	if !ok {
		t.Fatalf("expected AsDeviceError to succeed")
	}
	if devErr != t57.DevErrParamError {
		t.Fatalf("device error = % X, want 0x89", devErr)
	}
}

func TestClientWriteBlock(t *testing.T) {
	m := mock.New()
	// Device confirms with status=0x00, data=0x80 (per spec).
	m.PushResponse(buildResponse(0, 0x00, 0x80))

	c := t57.NewClient(m).WithRetries(0)
	blk := [t57.BlockSize]byte{0xCA, 0xFE, 0xBA, 0xBE}
	if err := c.WriteBlock(1, blk); err != nil {
		t.Fatalf("WriteBlock: %v", err)
	}
}

func TestClientReadBlocks(t *testing.T) {
	m := mock.New()
	// Two blocks back-to-back.
	m.PushResponse(buildResponse(0, 0x00, 1, 2, 3, 4))
	m.PushResponse(buildResponse(0, 0x00, 5, 6, 7, 8))

	c := t57.NewClient(m).WithRetries(0)
	blks, err := c.ReadBlocks(1, 2)
	if err != nil {
		t.Fatalf("ReadBlocks: %v", err)
	}
	want1 := [t57.BlockSize]byte{1, 2, 3, 4}
	want2 := [t57.BlockSize]byte{5, 6, 7, 8}
	if blks[0] != want1 || blks[1] != want2 {
		t.Fatalf("got (% X, % X), want (% X, % X)",
			blks[0], blks[1], want1, want2)
	}
}

func TestClientReadConfig(t *testing.T) {
	m := mock.New()
	// Build a config-block response: data = 0xE8, 0x80, 0x08, 0x00
	// (little-endian 0x000880E8).
	m.PushResponse(buildResponse(0, 0x00, 0xE8, 0x80, 0x08, 0x00))

	c := t57.NewClient(m).WithRetries(0)
	cfg, err := c.ReadConfig()
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if cfg.Bits() != 0x000880E8 {
		t.Fatalf("bits = %08X, want 0x000880E8", cfg.Bits())
	}
}

func TestClientWriteConfig(t *testing.T) {
	m := mock.New()
	m.PushResponse(buildResponse(0, 0x00, 0x80))

	c := t57.NewClient(m).WithRetries(0)
	cfg := t57.FactoryDefault()
	if err := c.WriteConfig(cfg); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
}

func TestClientRetriesOnTransientError(t *testing.T) {
	m := mock.New()
	// First response: bad checksum (BCC corrupted). Second: good.
	good := buildResponse(0, 0x00, 1, 2, 3, 4, 5, 6, 7, 8)
	bad := append([]byte{t57.STX, 0x00, 0x09, 0x00, 1, 2, 3, 4, 5, 6, 7, 8}, 0xFF, t57.ETX)
	// bad has the wrong BCC; the test is that the client retries.
	m.PushResponse(bad)
	m.PushResponse(good)

	c := t57.NewClient(m).WithRetries(2)
	got, err := c.SerialNumber()
	if err != nil {
		t.Fatalf("SerialNumber: %v", err)
	}
	want := [t57.SerialSize]byte{1, 2, 3, 4, 5, 6, 7, 8}
	if got != want {
		t.Fatalf("got % X, want % X", got, want)
	}
	if m.WriteCount() != 2 {
		t.Fatalf("write count = %d, want 2 (initial + 1 retry)", m.WriteCount())
	}
}

func TestClientRetriesExhausted(t *testing.T) {
	m := mock.New()
	// Always respond with bad checksum.
	bad := []byte{t57.STX, 0x00, 0x09, 0x00, 1, 2, 3, 4, 5, 6, 7, 8, 0xFF, t57.ETX}
	for i := 0; i < 5; i++ {
		m.PushResponse(bad)
	}

	c := t57.NewClient(m).WithRetries(2)
	_, err := c.SerialNumber()
	if !errors.Is(err, t57.ErrBadChecksum) {
		t.Fatalf("err = %v, want ErrBadChecksum", err)
	}
	if m.WriteCount() != 3 {
		t.Fatalf("write count = %d, want 3 (initial + 2 retries)", m.WriteCount())
	}
}

func TestClientSetBaud(t *testing.T) {
	m := mock.New()
	// Device echoes the rate code.
	m.PushResponse(buildResponse(0, 0x00, byte(t57.Baud19200)))

	c := t57.NewClient(m).WithRetries(0)
	if err := c.SetBaud(t57.Baud19200); err != nil {
		t.Fatalf("SetBaud: %v", err)
	}
}

func TestClientSetAddress(t *testing.T) {
	m := mock.New()
	m.PushResponse(buildResponse(0, 0x00, 0x05))

	c := t57.NewClient(m).WithRetries(0)
	if err := c.SetAddress(0x05); err != nil {
		t.Fatalf("SetAddress: %v", err)
	}
}

func TestClientFirmwareVersion(t *testing.T) {
	m := mock.New()
	m.PushResponse(buildResponse(0, 0x00, 'R', 'D', 'M', '5', '0', '0', '_', '0', '4', '0', '7'))

	c := t57.NewClient(m).WithRetries(0)
	got, err := c.FirmwareVersion()
	if err != nil {
		t.Fatalf("FirmwareVersion: %v", err)
	}
	if string(got) != "RDM500_0407" {
		t.Fatalf("got %q, want RDM500_0407", string(got))
	}
}

func TestClientEmptyReadIsError(t *testing.T) {
	m := mock.New()
	// Don't push any data; the device will never respond.
	c := t57.NewClient(m).WithRetries(0)
	_, err := c.SerialNumber()
	if !errors.Is(err, t57.ErrFrameTooShort) {
		t.Fatalf("err = %v, want ErrFrameTooShort", err)
	}
}
