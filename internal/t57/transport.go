package t57

import "io"

// Transport is the byte stream the protocol is layered on top of. The
// CLI ships a serial-port implementation; tests use a mock.
//
// Implementations are not required to be safe for concurrent use.
type Transport interface {
	// WriteAll writes the entire buffer, returning an error if not all
	// bytes could be sent.
	WriteAll(p []byte) (int, error)

	// Read reads up to len(p) bytes. It may return fewer bytes than
	// requested (and an io.EOF to indicate "no more data right now");
	// callers that need at-least-N semantics must loop.
	Read(p []byte) (int, error)

	// Flush blocks until any buffered bytes have left the device.
	Flush() error

	// Drain discards any bytes that have already been buffered by the
	// transport but not yet consumed.  This is used before sending a
	// new command to clear leftover data from a previous response.
	Drain() error

	// Close releases any resources held by the transport.
	Close() error
}

// ReadFull reads exactly `min` bytes, looping over short reads.
// Returns the number of bytes actually read (which may be less than
// `min` only if the transport returns io.EOF or 0).
func ReadFull(t Transport, p []byte, min int) (int, error) {
	if min > len(p) {
		return 0, makeErr("read_full", "buffer_too_small", ErrBufferTooSmall,
			map[string]any{"needed": min, "got": len(p)})
	}
	total := 0
	for total < min {
		n, err := t.Read(p[total:min])
		if n > 0 {
			total += n
		}
		if err != nil {
			if total > 0 && err == io.EOF {
				return total, nil
			}
			return total, makeErr("read_full", "io", err, nil)
		}
		if n == 0 {
			break
		}
	}
	return total, nil
}

// ReadFrame reads bytes from the transport until it sees an ETX (0xBB)
// marker or the buffer fills. Returns the number of bytes written to
// the buffer.
func ReadFrame(t Transport, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := t.Read(buf[total : total+1])
		if n > 0 {
			total += n
			if buf[total-1] == ETX {
				break
			}
			continue
		}
		// n == 0: either EOF or timeout.
		if err == nil || err == io.EOF {
			// Clean end-of-stream. If we got at least one byte,
			// hand back what we have. Otherwise, treat as a
			// frame-too-short.
			if total == 0 {
				return 0, makeErr("read_frame", "frame_too_short",
					ErrFrameTooShort,
					map[string]any{"received": 0, "minimum": MinFrame})
			}
			return total, nil
		}
		return total, makeErr("read_frame", "io", err, nil)
	}
	return total, nil
}
