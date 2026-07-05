package t57

import (
	"errors"
	"testing"
)

func TestHexBytes(t *testing.T) {
	cases := []struct {
		in   string
		want []byte
	}{
		{"DEADBEEF", []byte{0xDE, 0xAD, 0xBE, 0xEF}},
		{"deadbeef", []byte{0xDE, 0xAD, 0xBE, 0xEF}},
		{"0xDEADBEEF", []byte{0xDE, 0xAD, 0xBE, 0xEF}},
		{"DE AD BE EF", []byte{0xDE, 0xAD, 0xBE, 0xEF}},
		{"DE,AD,BE,EF", []byte{0xDE, 0xAD, 0xBE, 0xEF}},
		{"", []byte{}},
	}
	for _, c := range cases {
		got, err := HexBytes(c.in)
		if err != nil {
			t.Errorf("HexBytes(%q): %v", c.in, err)
			continue
		}
		if !bytesEqual(got, c.want) {
			t.Errorf("HexBytes(%q) = % X, want % X", c.in, got, c.want)
		}
	}
}

func TestHexBytesErrors(t *testing.T) {
	cases := []string{"ABC", "DEAXBEEF", "0xZZ"}
	for _, in := range cases {
		_, err := HexBytes(in)
		if !errors.Is(err, ErrHexParse) {
			t.Errorf("HexBytes(%q) err = %v, want ErrHexParse", in, err)
		}
	}
}

func TestParseBlock(t *testing.T) {
	blk, err := ParseBlock("DEADBEEF")
	if err != nil {
		t.Fatalf("ParseBlock: %v", err)
	}
	want := [4]byte{0xDE, 0xAD, 0xBE, 0xEF}
	if blk != want {
		t.Fatalf("got % X, want % X", blk, want)
	}
}

func TestParseBlockErrors(t *testing.T) {
	cases := []string{"", "ABCD", "DEADBEEF00"}
	for _, in := range cases {
		_, err := ParseBlock(in)
		if err == nil {
			t.Errorf("ParseBlock(%q) should have failed", in)
		}
	}
}

func TestFormatHex(t *testing.T) {
	got := FormatHex([]byte{0xDE, 0xAD, 0xBE, 0xEF})
	if got != "DEADBEEF" {
		t.Fatalf("FormatHex = %q, want DEADBEEF", got)
	}
}

func TestFormatBlock(t *testing.T) {
	got := FormatBlock([4]byte{0xDE, 0xAD, 0xBE, 0xEF})
	if got != "0xDEADBEEF" {
		t.Fatalf("FormatBlock = %q, want 0xDEADBEEF", got)
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
