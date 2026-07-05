package t57

import "errors"

// DeviceError is a typed error code returned by the device in the
// `DATA[0]` byte of a failed response. Codes come from §8 of the
// device protocol spec.
type DeviceError byte

// Known device error codes. The spec lists 0x86 and 0x87 as both
// "unknown error" but the device does emit both, so we keep them
// distinct.
const (
	DevErrOperationFailed DeviceError = 0x01
	DevErrParamSetOk      DeviceError = 0x80
	DevErrParamSetFailed  DeviceError = 0x81
	DevErrCommTimeout     DeviceError = 0x82
	DevErrNoCard          DeviceError = 0x83
	DevErrReceiveError    DeviceError = 0x84
	DevErrAuthFailed      DeviceError = 0x85
	DevErrUnknown86       DeviceError = 0x86
	DevErrUnknown87       DeviceError = 0x87
	DevErrParamError      DeviceError = 0x89
	DevErrUnknownCommand  DeviceError = 0x8F
)

// String returns a short, human-readable name for the error.
func (d DeviceError) String() string {
	switch d {
	case DevErrOperationFailed:
		return "operation failed"
	case DevErrParamSetOk:
		return "parameter set OK"
	case DevErrParamSetFailed:
		return "parameter set failed"
	case DevErrCommTimeout:
		return "communication timeout"
	case DevErrNoCard:
		return "no card in field"
	case DevErrReceiveError:
		return "receive error from card"
	case DevErrAuthFailed:
		return "authentication failed"
	case DevErrUnknown86, DevErrUnknown87:
		return "unknown error"
	case DevErrParamError:
		return "bad input parameter"
	case DevErrUnknownCommand:
		return "unknown command"
	}
	return "unknown device error"
}

// ParseDeviceError decodes the raw sub-code byte.
func ParseDeviceError(b byte) DeviceError {
	return DeviceError(b)
}

// AsDeviceError extracts the device error from an Error, if present.
// Returns the error and true on success, or zero and false otherwise.
func AsDeviceError(err error) (DeviceError, bool) {
	var e *Error
	if errors.As(err, &e) && e.Kind == "device_error" {
		if d, ok := e.Detail["device_error"].(DeviceError); ok {
			return d, true
		}
	}
	return 0, false
}
