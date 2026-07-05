package t57

import (
	"bytes"
	"errors"
	"testing"
)

func TestEncodeDecodeRoundtripEmpty(t *testing.T) {
	var buf [16]byte
	enc, err := Encode(buf[:], 0, CmdSysGetSerlNum, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if enc.Len != 6 {
		t.Fatalf("len = %d, want 6", enc.Len)
	}
	// BCC = 0x00 ^ 0x01 ^ 0x83 = 0x82
	want := []byte{STX, 0x00, 0x01, byte(CmdSysGetSerlNum), 0x82, ETX}
	if !bytes.Equal(buf[:6], want) {
		t.Fatalf("encoded = % X, want % X", buf[:6], want)
	}
	dec, err := Decode(buf[:6])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dec.Status != byte(CmdSysGetSerlNum) {
		t.Fatalf("status = % X, want 0x83", dec.Status)
	}
	if len(dec.Data) != 0 {
		t.Fatalf("data = % X, want empty", dec.Data)
	}
}

func TestEncodeDecodeRoundtripWithData(t *testing.T) {
	var buf [32]byte
	payload := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x00}
	enc, err := Encode(buf[:], 0, CmdT5557Write, payload)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if enc.Len != 12 {
		t.Fatalf("len = %d, want 12", enc.Len)
	}
	// Manually compute BCC = XOR of bytes 1..10.
	bcc := byte(0)
	for i := 1; i < 10; i++ {
		bcc ^= buf[i]
	}
	want := []byte{STX, 0x00, 0x07, byte(CmdT5557Write),
		0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x00, bcc, ETX}
	if !bytes.Equal(buf[:12], want) {
		t.Fatalf("encoded = % X, want % X", buf[:12], want)
	}
	dec, err := Decode(buf[:12])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dec.Status != byte(CmdT5557Write) {
		t.Fatalf("status = % X, want 0x91", dec.Status)
	}
	if !bytes.Equal(dec.Data, payload) {
		t.Fatalf("data = % X, want % X", dec.Data, payload)
	}
}

func TestEncodePayloadTooLarge(t *testing.T) {
	var buf [300]byte
	big := make([]byte, MaxData+1)
	_, err := Encode(buf[:], 0, CmdSysGetSerlNum, big)
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("err = %v, want ErrPayloadTooLarge", err)
	}
}

func TestEncodeBufferTooSmall(t *testing.T) {
	var buf [4]byte
	_, err := Encode(buf[:], 0, CmdSysGetSerlNum, nil)
	if !errors.Is(err, ErrBufferTooSmall) {
		t.Fatalf("err = %v, want ErrBufferTooSmall", err)
	}
}

func TestDecodeRejectsBadStart(t *testing.T) {
	var buf [16]byte
	Encode(buf[:], 0, CmdSysGetSerlNum, nil)
	buf[0] = 0x55
	_, err := Decode(buf[:6])
	if !errors.Is(err, ErrBadStartMarker) {
		t.Fatalf("err = %v, want ErrBadStartMarker", err)
	}
}

func TestDecodeRejectsBadEnd(t *testing.T) {
	var buf [16]byte
	Encode(buf[:], 0, CmdSysGetSerlNum, nil)
	buf[5] = 0x55
	_, err := Decode(buf[:6])
	if !errors.Is(err, ErrBadEndMarker) {
		t.Fatalf("err = %v, want ErrBadEndMarker", err)
	}
}

func TestDecodeRejectsBadChecksum(t *testing.T) {
	var buf [16]byte
	Encode(buf[:], 0, CmdSysGetSerlNum, nil)
	buf[4] ^= 0x01
	_, err := Decode(buf[:6])
	if !errors.Is(err, ErrBadChecksum) {
		t.Fatalf("err = %v, want ErrBadChecksum", err)
	}
}

func TestDecodeRejectsTruncated(t *testing.T) {
	var buf [16]byte
	Encode(buf[:], 0, CmdSysGetSerlNum, nil)
	_, err := Decode(buf[:3])
	if !errors.Is(err, ErrFrameTooShort) {
		t.Fatalf("err = %v, want ErrFrameTooShort", err)
	}
}

func TestBCCKnown(t *testing.T) {
	var expected byte
	for i := byte(0); i < 10; i++ {
		expected ^= i
	}
	if got := BCC([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}); got != expected {
		t.Fatalf("BCC = % X, want % X", got, expected)
	}
}

func TestBCCEmpty(t *testing.T) {
	if got := BCC(nil); got != 0 {
		t.Fatalf("BCC(nil) = % X, want 0", got)
	}
}

func TestEncodeSpecExample(t *testing.T) {
	// From §3.1.4 of the spec:
	//   "AA 02 01 83 80 BB" — SysGetSerlNum
	//   "AA 02 09 00 AA BB AA BB AA BB AA BB 0B BB" — response
	var tx [16]byte
	enc, err := Encode(tx[:], 0x02, CmdSysGetSerlNum, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if enc.Len != 6 {
		t.Fatalf("len = %d, want 6", enc.Len)
	}
	want := []byte{0xAA, 0x02, 0x01, 0x83, 0x80, 0xBB}
	if !bytes.Equal(tx[:6], want) {
		t.Fatalf("encoded = % X, want % X", tx[:6], want)
	}
	// Decode the spec's response.
	resp := []byte{0xAA, 0x02, 0x09, 0x00, 0xAA, 0xBB, 0xAA, 0xBB, 0xAA, 0xBB, 0xAA, 0xBB, 0x0B, 0xBB}
	dec, err := Decode(resp)
	if err != nil {
		t.Fatalf("decode spec example: %v", err)
	}
	if dec.Station != 0x02 {
		t.Fatalf("station = % X, want 0x02", dec.Station)
	}
	if dec.Status != 0x00 {
		t.Fatalf("status = % X, want 0x00", dec.Status)
	}
	wantData := []byte{0xAA, 0xBB, 0xAA, 0xBB, 0xAA, 0xBB, 0xAA, 0xBB}
	if !bytes.Equal(dec.Data, wantData) {
		t.Fatalf("data = % X, want % X", dec.Data, wantData)
	}
}
