package t57

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// HexBytes converts a hex string ("DEADBEEF" or "DE AD BE EF" or
// "0x...") into bytes.
func HexBytes(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	// Strip whitespace and commas.
	var b strings.Builder
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', ',':
			continue
		}
		b.WriteRune(r)
	}
	clean := b.String()
	out, err := hex.DecodeString(clean)
	if err != nil {
		return nil, makeErr("hex_bytes", "hex_parse", ErrHexParse,
			map[string]any{"input": s})
	}
	return out, nil
}

// FormatHex returns the uppercase hex encoding of `b`.
func FormatHex(b []byte) string {
	return strings.ToUpper(hex.EncodeToString(b))
}

// ParseBlock parses a 4-byte hex string into a [4]byte block. Returns
// an error if the input is not exactly 8 hex digits.
func ParseBlock(s string) ([BlockSize]byte, error) {
	var out [BlockSize]byte
	raw, err := HexBytes(s)
	if err != nil {
		return out, err
	}
	if len(raw) != BlockSize {
		return out, makeErr("parse_block", "buffer_too_small", ErrBufferTooSmall,
			map[string]any{"needed": BlockSize, "got": len(raw)})
	}
	copy(out[:], raw)
	return out, nil
}

// FormatBlock is the inverse of ParseBlock.
func FormatBlock(b [BlockSize]byte) string {
	return fmt.Sprintf("0x%08X", b)
}
