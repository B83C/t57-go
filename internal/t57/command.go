package t57

// Command is a wire-protocol command code. Values match what the T57
// firmware uses.
//
// Only the commands the CLI currently uses are enumerated. The full set
// of commands the firmware supports is in §3-§7 of the device protocol
// spec.
type Command byte

// Wire command codes.
const (
	// System commands (§3 of the spec).
	CmdSysSetAddress   Command = 0x80
	CmdSysSetBaudrate  Command = 0x81
	CmdSysSetSerlNum   Command = 0x82
	CmdSysGetSerlNum   Command = 0x83
	CmdSysWriteUserInfo Command = 0x84
	CmdSysReadUserInfo  Command = 0x85
	CmdSysGetVersion    Command = 0x86
	CmdSysControlUserPort Command = 0x87
	CmdSysControlLed    Command = 0x88
	CmdSysControlBuzzer Command = 0x89

	// T5557 commands (§7 of the spec).
	CmdT5557Read  Command = 0x90
	CmdT5557Write Command = 0x91
)

// IsKnown reports whether the command code is one this package has a
// typed name for. Unknown codes are still accepted by Client.Transact.
func (c Command) IsKnown() bool {
	switch c {
	case CmdSysSetAddress, CmdSysSetBaudrate, CmdSysSetSerlNum,
		CmdSysGetSerlNum, CmdSysWriteUserInfo, CmdSysReadUserInfo,
		CmdSysGetVersion, CmdSysControlUserPort, CmdSysControlLed,
		CmdSysControlBuzzer,
		CmdT5557Read, CmdT5557Write:
		return true
	}
	return false
}

// BaudCode is the encoding of baud-rate choices the device accepts on
// the SysSetBaudrate command.
type BaudCode byte

// Baud-rate codes.
const (
	Baud9600   BaudCode = 0x00
	Baud19200  BaudCode = 0x01
	Baud38400  BaudCode = 0x02
	Baud57600  BaudCode = 0x03
	Baud115200 BaudCode = 0x04
)

// Baud returns the numeric baud rate for the given code.
func (b BaudCode) Baud() int {
	switch b {
	case Baud9600:
		return 9600
	case Baud19200:
		return 19200
	case Baud38400:
		return 38400
	case Baud57600:
		return 57600
	case Baud115200:
		return 115200
	}
	return 0
}

// ParseBaud finds the BaudCode for a numeric rate. Returns the code and
// ok=false if the rate is not supported.
func ParseBaud(rate int) (BaudCode, bool) {
	switch rate {
	case 9600:
		return Baud9600, true
	case 19200:
		return Baud19200, true
	case 38400:
		return Baud38400, true
	case 57600:
		return Baud57600, true
	case 115200:
		return Baud115200, true
	}
	return 0, false
}

// Page is the T5557 page number.
type Page byte

// T5557 pages.
const (
	Page0 Page = 0 // config + user blocks 1..=7
	Page1 Page = 1 // lock bits / extra config
)
