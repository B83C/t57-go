package mock_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/t57/t57-go/internal/mock"
)

func TestMockBasicReadWrite(t *testing.T) {
	m := mock.New()
	m.PushResponse([]byte{0x01, 0x02, 0x03})

	if n, err := m.WriteAll([]byte{0xAA, 0xBB}); err != nil || n != 2 {
		t.Fatalf("WriteAll: n=%d err=%v", n, err)
	}

	buf := make([]byte, 3)
	n, err := m.Read(buf)
	if err != nil || n != 3 {
		t.Fatalf("Read: n=%d err=%v", n, err)
	}
	if !bytes.Equal(buf, []byte{1, 2, 3}) {
		t.Fatalf("got % X", buf)
	}
}

func TestMockReadEmptyReturnsEOF(t *testing.T) {
	m := mock.New()
	buf := make([]byte, 4)
	n, err := m.Read(buf)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want EOF", err)
	}
	if n != 0 {
		t.Fatalf("n = %d, want 0", n)
	}
}

func TestMockReadAfterCloseReturnsClosedPipe(t *testing.T) {
	m := mock.New()
	m.Close()
	_, err := m.Read(make([]byte, 4))
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("err = %v, want ErrClosedPipe", err)
	}
}

func TestMockWriteAfterCloseReturnsClosedPipe(t *testing.T) {
	m := mock.New()
	m.Close()
	_, err := m.WriteAll([]byte{0x01})
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("err = %v, want ErrClosedPipe", err)
	}
}

func TestMockSetReadError(t *testing.T) {
	m := mock.New()
	m.SetReadError(mock.ErrTimeout)
	_, err := m.Read(make([]byte, 4))
	if !errors.Is(err, mock.ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
}

func TestMockSetWriteError(t *testing.T) {
	m := mock.New()
	m.SetWriteError(mock.ErrTimeout)
	_, err := m.WriteAll([]byte{0x01})
	if !errors.Is(err, mock.ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
}

func TestMockCounts(t *testing.T) {
	m := mock.New()
	m.PushResponse([]byte{0x01, 0x02, 0x03})

	m.WriteAll([]byte{0xAA})
	m.Read(make([]byte, 1))
	m.Read(make([]byte, 1))

	if m.WriteCount() != 1 {
		t.Fatalf("write count = %d, want 1", m.WriteCount())
	}
	if m.ReadCount() != 2 {
		t.Fatalf("read count = %d, want 2", m.ReadCount())
	}
}

func TestMockTxBytes(t *testing.T) {
	m := mock.New()
	m.WriteAll([]byte{1, 2, 3})
	m.WriteAll([]byte{4, 5})
	if !bytes.Equal(m.TxBytes(), []byte{1, 2, 3, 4, 5}) {
		t.Fatalf("tx = % X", m.TxBytes())
	}
}

func TestMockEcho(t *testing.T) {
	m := mock.New()
	m.SetEcho(true)
	m.WriteAll([]byte{1, 2, 3})
	buf := make([]byte, 3)
	if n, _ := m.Read(buf); n != 3 {
		t.Fatalf("echo read n = %d, want 3", n)
	}
	if !bytes.Equal(buf, []byte{1, 2, 3}) {
		t.Fatalf("echo data = % X", buf)
	}
}
