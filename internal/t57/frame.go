package t57

// EncodedFrame is the result of an Encode call.
type EncodedFrame struct {
	// Len is the number of bytes written, including the STX/ETX markers.
	Len int
}

// Debug logging hook. Set to non-nil to receive hex dumps of every
// frame encode/decode.
var DebugLog func(msg string, args ...interface{})

func debugf(msg string, args ...interface{}) {
	if DebugLog != nil {
		DebugLog(msg, args...)
	}
}

// Encode writes a frame into `buf` and returns the number of bytes
// used. The frame layout is:
//
//	[STX, station, length, command, data..., bcc, ETX]
//
// where `length` = 1 + len(data) (the command byte is included),
// and `bcc` is the XOR of every byte from `station` to the last data
// byte.
//
// Returns ErrPayloadTooLarge if `len(data) > MaxData` and
// ErrBufferTooSmall if `buf` is too small for the resulting frame.
func Encode(buf []byte, station byte, cmd Command, data []byte) (EncodedFrame, error) {
	dataLen := len(data)
	if dataLen > MaxData {
		return EncodedFrame{}, makeErr("encode", "payload_too_large", ErrPayloadTooLarge,
			map[string]any{"len": dataLen, "max": MaxData})
	}
	// Frame is STX(1) + station(1) + length(1) + cmd(1) + data(dataLen) + BCC(1) + ETX(1)
	// = 6 + dataLen bytes.
	total := 6 + dataLen
	if len(buf) < total {
		return EncodedFrame{}, makeErr("encode", "buffer_too_small", ErrBufferTooSmall,
			map[string]any{"needed": total, "got": len(buf)})
	}
	buf[0] = STX
	buf[1] = station
	buf[2] = byte(1 + dataLen)
	buf[3] = byte(cmd)
	copy(buf[4:4+dataLen], data)

	bcc := byte(0)
	for i := 1; i < 4+dataLen; i++ {
		bcc ^= buf[i]
	}
	buf[4+dataLen] = bcc
	buf[5+dataLen] = ETX

	debugf("encode station=0x%02X cmd=0x%02X data=[% X] → [% X]",
		station, cmd, data, buf[:total])
	return EncodedFrame{Len: total}, nil
}

// Decoded is the result of a successful Decode.
type Decoded struct {
	// Station is the station address byte from the response.
	Station byte
	// Status is the response status byte (0x00 = success).
	Status byte
	// Data is the response payload (everything between the status byte
	// and the BCC).
	Data []byte
}

// Decode parses a response frame from `buf`. The buffer may be longer
// than the frame; trailing bytes are ignored.
//
// Returns one of ErrFrameTooShort, ErrBadStartMarker, ErrBadEndMarker,
// ErrLengthMismatch, or ErrBadChecksum on failure.
func Decode(buf []byte) (Decoded, error) {
	if len(buf) < MinFrame {
		return Decoded{}, makeErr("decode", "frame_too_short", ErrFrameTooShort,
			map[string]any{"received": len(buf), "minimum": MinFrame})
	}
	if buf[0] != STX {
		return Decoded{}, makeErr("decode", "bad_start_marker", ErrBadStartMarker,
			map[string]any{"got": buf[0]})
	}
	length := int(buf[2])
	dataLen := length - 1
	if dataLen < 0 {
		dataLen = 0
	}
	// Frame layout:
	//   buf[0]             STX
	//   buf[1]             station
	//   buf[2]             length (= 1 + dataLen)
	//   buf[3]             cmd / status
	//   buf[4..4+dataLen]  payload
	//   buf[4+dataLen]     BCC
	//   buf[5+dataLen]     ETX
	bccPos := 4 + dataLen
	lastPos := bccPos + 1
	if len(buf) <= lastPos {
		return Decoded{}, makeErr("decode", "frame_too_short", ErrFrameTooShort,
			map[string]any{"received": len(buf), "minimum": lastPos + 1})
	}
	if buf[lastPos] != ETX {
		return Decoded{}, makeErr("decode", "bad_end_marker", ErrBadEndMarker,
			map[string]any{"got": buf[lastPos]})
	}

	bcc := byte(0)
	for i := 1; i < bccPos; i++ {
		bcc ^= buf[i]
	}
	if bcc != buf[bccPos] {
		return Decoded{}, makeErr("decode", "bad_checksum", ErrBadChecksum,
			map[string]any{"received": buf[bccPos], "expected": bcc})
	}

	declared := length - 1
	if declared != dataLen {
		return Decoded{}, makeErr("decode", "length_mismatch", ErrLengthMismatch,
			map[string]any{"declared": declared, "actual": dataLen})
	}

	debugf("decode status=0x%02X data=[% X] (length=%d bccPos=%d n=%d)",
		buf[3], buf[4:4+dataLen], length, bccPos, len(buf))

	return Decoded{
		Station: buf[1],
		Status:  buf[3],
		Data:    buf[4 : 4+dataLen],
	}, nil
}

// BCC computes the XOR checksum of an arbitrary byte slice. Public so
// callers building raw frames (e.g. for fuzz tests) can use the same
// definition the package uses internally.
func BCC(data []byte) byte {
	var x byte
	for _, b := range data {
		x ^= b
	}
	return x
}
