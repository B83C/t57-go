package t57

import (
	"errors"
	"fmt"
)

// Error is the type returned by every fallible call in the t57 package.
//
// The wrapping design (`Error` holds an `error`) keeps the public API
// flat: callers can match on the variant with errors.As and unwrap the
// underlying transport error, or just call Error() for a human-readable
// summary.
type Error struct {
	// Op names the operation that produced the error, e.g. "encode".
	Op string
	// Kind is a short, machine-readable code, e.g. "bad_checksum".
	Kind string
	// Cause is the underlying transport or I/O error, if any.
	Cause error
	// Detail is a small structured payload describing the error.
	Detail map[string]any
}

func (e *Error) Error() string {
	op := e.Op
	if op == "" {
		op = "t57"
	}
	base := fmt.Sprintf("%s: %s", op, e.Kind)
	if e.Cause != nil {
		base += ": " + e.Cause.Error()
	}
	if len(e.Detail) > 0 {
		base += fmt.Sprintf(" [%v]", e.Detail)
	}
	return base
}

// Unwrap returns the underlying cause, if any. Use with errors.Is and
// errors.As.
func (e *Error) Unwrap() error {
	return e.Cause
}

// IsTransient reports whether the error looks like a wire-level hiccup
// worth retrying. Callers can use this to drive a retry loop.
func (e *Error) IsTransient() bool {
	switch e.Kind {
	case "frame_too_short", "bad_start_marker", "bad_end_marker",
		"length_mismatch", "bad_checksum", "io":
		return true
	}
	return false
}

// Is reports whether this error matches `target` either by kind match
// (using the special error sentinels below) or by chain unwrap.
func (e *Error) Is(target error) bool {
	for _, s := range sentinels {
		if errors.Is(target, s) && target == s && e.Kind == s.kind {
			return true
		}
	}
	return false
}

// Sentinel errors. Use these with errors.Is to identify a class of
// failure regardless of detail.
type sentinel struct{ kind string }

func (s sentinel) Error() string { return "t57: " + s.kind }
func (s sentinel) Is(target error) bool {
	t, ok := target.(sentinel)
	return ok && t == s
}

var sentinels []sentinel

func newSentinel(kind string) sentinel {
	s := sentinel{kind}
	sentinels = append(sentinels, s)
	return s
}

// Common error kinds.
var (
	ErrFrameTooShort    = newSentinel("frame_too_short")
	ErrBadStartMarker   = newSentinel("bad_start_marker")
	ErrBadEndMarker     = newSentinel("bad_end_marker")
	ErrLengthMismatch   = newSentinel("length_mismatch")
	ErrBadChecksum      = newSentinel("bad_checksum")
	ErrDeviceStatus     = newSentinel("device_status")
	ErrDeviceError      = newSentinel("device_error")
	ErrPayloadTooLarge  = newSentinel("payload_too_large")
	ErrBufferTooSmall   = newSentinel("buffer_too_small")
	ErrOutOfRange       = newSentinel("out_of_range")
	ErrHexParse         = newSentinel("hex_parse")
	ErrNoDevice         = newSentinel("no_device")
	ErrPortNotFound     = newSentinel("port_not_found")
	ErrIO               = newSentinel("io")
)

// makeErr is a tiny constructor so the call sites stay compact.
func makeErr(op, kind string, cause error, detail map[string]any) *Error {
	if detail == nil {
		detail = map[string]any{}
	}
	return &Error{Op: op, Kind: kind, Cause: cause, Detail: detail}
}
