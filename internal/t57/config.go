package t57

import "encoding/binary"

// DataBitRate is the RF data rate field of the config block.
type DataBitRate byte

// RF data bit rates.
const (
	BitRateRF8   DataBitRate = 0
	BitRateRF16  DataBitRate = 1
	BitRateRF32  DataBitRate = 2
	BitRateRF40  DataBitRate = 3
	BitRateRF50  DataBitRate = 4
	BitRateRF64  DataBitRate = 5
	BitRateRF100 DataBitRate = 6
	BitRateRF128 DataBitRate = 7
)

func (d DataBitRate) String() string {
	switch d {
	case BitRateRF8:
		return "RF/8"
	case BitRateRF16:
		return "RF/16"
	case BitRateRF32:
		return "RF/32"
	case BitRateRF40:
		return "RF/40"
	case BitRateRF50:
		return "RF/50"
	case BitRateRF64:
		return "RF/64"
	case BitRateRF100:
		return "RF/100"
	case BitRateRF128:
		return "RF/128"
	}
	return "unknown"
}

// Modulation is the modulation field of the config block.
type Modulation byte

// Modulation schemes.
const (
	ModDirect     Modulation = 0
	ModPSK1       Modulation = 1
	ModPSK2       Modulation = 2
	ModPSK3       Modulation = 3
	ModFSK1       Modulation = 4
	ModFSK2       Modulation = 5
	ModFSK1a      Modulation = 6
	ModFSK2a      Modulation = 7
	ModManchester Modulation = 8
	ModBiPhase    Modulation = 9
)

func (m Modulation) String() string {
	switch m {
	case ModDirect:
		return "direct"
	case ModPSK1:
		return "PSK1"
	case ModPSK2:
		return "PSK2"
	case ModPSK3:
		return "PSK3"
	case ModFSK1:
		return "FSK1"
	case ModFSK2:
		return "FSK2"
	case ModFSK1a:
		return "FSK1a"
	case ModFSK2a:
		return "FSK2a"
	case ModManchester:
		return "manchester"
	case ModBiPhase:
		return "bi-phase"
	}
	return "unknown"
}

// PskCf is the PSK carrier-frequency divider field.
type PskCf byte

// PSK carrier dividers.
const (
	PskCfRF2 PskCf = 0
	PskCfRF4 PskCf = 1
	PskCfRF8 PskCf = 2
)

func (p PskCf) String() string {
	switch p {
	case PskCfRF2:
		return "RF/2"
	case PskCfRF4:
		return "RF/4"
	case PskCfRF8:
		return "RF/8"
	}
	return "unknown"
}

// Config is the 32-bit configuration block stored at block 0.
//
// The bit layout matches the device firmware:
//
//	bit  0..3   master_key        (4 bits)
//	bit  4..10  padding1          (7 bits, ignored by firmware)
//	bit 11..13  data_bit_rate     (3 bits)
//	bit 14      padding2          (1 bit)
//	bit 15..19  modulation        (5 bits)
//	bit 20..21  pskcf             (2 bits)
//	bit 22      aor               (1 bit)
//	bit 23      padding3          (1 bit)
//	bit 24..26  max_block         (3 bits)
//	bit 27      pwd               (1 bit)
//	bit 28      st_seq_terminator (1 bit)
//	bit 29..30  padding4          (2 bits)
//	bit 31      init_delay        (1 bit)
//
// Padding fields are preserved across round-trips so that writes don't
// accidentally flip reserved bits.
type Config struct {
	b uint32
}

// NewConfig returns a zeroed Config.
func NewConfig() Config { return Config{} }

// ConfigFromBits wraps a raw 32-bit value. The caller is responsible
// for whatever bit-layout convention is being used.
func ConfigFromBits(b uint32) Config { return Config{b: b} }

// Bits returns the raw 32-bit value.
func (c Config) Bits() uint32 { return c.b }

// ConfigFromLEBytes interprets `b` as a little-endian 32-bit word.
func ConfigFromLEBytes(b [4]byte) Config {
	return Config{b: binary.LittleEndian.Uint32(b[:])}
}

// LEBytes returns the config as a little-endian 4-byte array.
func (c Config) LEBytes() [4]byte {
	var out [4]byte
	binary.LittleEndian.PutUint32(out[:], c.b)
	return out
}

// ConfigFromBEBytes interprets `b` as a big-endian 32-bit word.
func ConfigFromBEBytes(b [4]byte) Config {
	return Config{b: binary.BigEndian.Uint32(b[:])}
}

// BEBytes returns the config as a big-endian 4-byte array.
func (c Config) BEBytes() [4]byte {
	var out [4]byte
	binary.BigEndian.PutUint32(out[:], c.b)
	return out
}

// --- field accessors ---

// MasterKey returns the 4-bit master-key field.
func (c Config) MasterKey() uint8 { return uint8(c.b & 0x0F) }

// SetMasterKey updates the master-key field. Only the low 4 bits of v
// are kept.
func (c *Config) SetMasterKey(v uint8) {
	c.b = (c.b &^ 0x0F) | (uint32(v) & 0x0F)
}

// DataBitRate returns the data-bit-rate field.
func (c Config) DataBitRate() DataBitRate { return DataBitRate((c.b >> 11) & 0x07) }

// SetDataBitRate updates the data-bit-rate field.
func (c *Config) SetDataBitRate(v DataBitRate) {
	c.b = (c.b &^ (0x07 << 11)) | (uint32(v&0x07) << 11)
}

// Modulation returns the modulation field.
func (c Config) Modulation() Modulation { return Modulation((c.b >> 15) & 0x1F) }

// SetModulation updates the modulation field.
func (c *Config) SetModulation(v Modulation) {
	c.b = (c.b &^ (0x1F << 15)) | (uint32(v&0x1F) << 15)
}

// PskCf returns the PSK carrier-frequency divider field.
func (c Config) PskCf() PskCf { return PskCf((c.b >> 20) & 0x03) }

// SetPskCf updates the PSK carrier-frequency divider field.
func (c *Config) SetPskCf(v PskCf) {
	c.b = (c.b &^ (0x03 << 20)) | (uint32(v&0x03) << 20)
}

// AOR returns the Answer-On-Request bit.
func (c Config) AOR() bool { return c.b&(1<<22) != 0 }

// SetAOR sets the Answer-On-Request bit.
func (c *Config) SetAOR(on bool) {
	if on {
		c.b |= 1 << 22
	} else {
		c.b &^= 1 << 22
	}
}

// MaxBlock returns the max-block field (3 bits).
func (c Config) MaxBlock() uint8 { return uint8((c.b >> 24) & 0x07) }

// SetMaxBlock updates the max-block field. Returns ErrOutOfRange if v
// exceeds 7.
func (c *Config) SetMaxBlock(v uint8) error {
	if v > MaxBlock {
		return makeErr("set_max_block", "out_of_range", ErrOutOfRange,
			map[string]any{"field": "max_block", "value": v, "max": MaxBlock})
	}
	c.b = (c.b &^ (0x07 << 24)) | (uint32(v&0x07) << 24)
	return nil
}

// PWD returns the password-protect bit.
func (c Config) PWD() bool { return c.b&(1<<27) != 0 }

// SetPWD sets the password-protect bit.
func (c *Config) SetPWD(on bool) {
	if on {
		c.b |= 1 << 27
	} else {
		c.b &^= 1 << 27
	}
}

// STSequenceTerminator returns the ST-sequence-terminator bit.
func (c Config) STSequenceTerminator() bool { return c.b&(1<<28) != 0 }

// SetSTSequenceTerminator sets the ST-sequence-terminator bit.
func (c *Config) SetSTSequenceTerminator(on bool) {
	if on {
		c.b |= 1 << 28
	} else {
		c.b &^= 1 << 28
	}
}

// InitDelay returns the init-delay bit.
func (c Config) InitDelay() bool { return c.b&(1<<31) != 0 }

// SetInitDelay sets the init-delay bit.
func (c *Config) SetInitDelay(on bool) {
	if on {
		c.b |= 1 << 31
	} else {
		c.b &^= 1 << 31
	}
}

// Padding1 returns the 7-bit padding1 field. The firmware ignores this,
// but it is preserved across round-trips.
func (c Config) Padding1() uint8 { return uint8((c.b >> 4) & 0x7F) }

// SetPadding1 updates the padding1 field.
func (c *Config) SetPadding1(v uint8) {
	c.b = (c.b &^ (0x7F << 4)) | (uint32(v&0x7F) << 4)
}

// FactoryDefault returns the config that ships with the device. Matches
// the value 0x000880E8 from the original Zig code, formatted as a
// big-endian word.
func FactoryDefault() Config {
	return Config{b: 0x0008_80E8}
}

// String returns a human-readable summary of the config.
func (c Config) String() string {
	raw := c.LEBytes()
	return "Config{" +
		"master_key=0x" + hexByte(c.MasterKey()) +
		", data_bit_rate=" + c.DataBitRate().String() +
		", modulation=" + c.Modulation().String() +
		", pskcf=" + c.PskCf().String() +
		", aor=" + boolStr(c.AOR()) +
		", max_block=" + uintStr(c.MaxBlock()) +
		", pwd=" + boolStr(c.PWD()) +
		", st=" + boolStr(c.STSequenceTerminator()) +
		", init_delay=" + boolStr(c.InitDelay()) +
		", raw=0x" + FormatHex(raw[:]) +
		"}"
}

func hexByte(b uint8) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{digits[b>>4], digits[b&0xF]})
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func uintStr(n uint8) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{digits[0]}, digits...) // shift right
		_ = digits
		const d = "0123456789"
		digits = append([]byte{d[n%10]}, digits...)
		n /= 10
	}
	return string(digits)
}
