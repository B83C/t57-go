package t57

import (
	"errors"
	"testing"
)

func TestConfigFromLEBytes(t *testing.T) {
	// 0x000880E8 little-endian.
	c := ConfigFromLEBytes([4]byte{0xE8, 0x80, 0x08, 0x00})
	if c.Bits() != 0x000880E8 {
		t.Fatalf("bits = %08X, want 0x000880E8", c.Bits())
	}
}

func TestConfigFactoryDefault(t *testing.T) {
	if FactoryDefault().Bits() != 0x000880E8 {
		t.Fatalf("factory default bits = %08X, want 0x000880E8",
			FactoryDefault().Bits())
	}
}

func TestConfigFieldAccessors(t *testing.T) {
	c := NewConfig()
	c.SetMasterKey(0xA)
	c.SetDataBitRate(BitRateRF32)
	c.SetModulation(ModManchester)
	c.SetPskCf(PskCfRF8)
	c.SetAOR(true)
	if err := c.SetMaxBlock(6); err != nil {
		t.Fatalf("set max block: %v", err)
	}
	c.SetPWD(true)
	c.SetSTSequenceTerminator(true)
	c.SetInitDelay(true)
	c.SetPadding1(0x55)

	if c.MasterKey() != 0xA {
		t.Errorf("master_key = %X, want 0xA", c.MasterKey())
	}
	if c.DataBitRate() != BitRateRF32 {
		t.Errorf("data_bit_rate = %v, want RF/32", c.DataBitRate())
	}
	if c.Modulation() != ModManchester {
		t.Errorf("modulation = %v, want manchester", c.Modulation())
	}
	if c.PskCf() != PskCfRF8 {
		t.Errorf("pskcf = %v, want RF/8", c.PskCf())
	}
	if !c.AOR() {
		t.Errorf("aor = false, want true")
	}
	if c.MaxBlock() != 6 {
		t.Errorf("max_block = %d, want 6", c.MaxBlock())
	}
	if !c.PWD() {
		t.Errorf("pwd = false, want true")
	}
	if !c.STSequenceTerminator() {
		t.Errorf("st = false, want true")
	}
	if !c.InitDelay() {
		t.Errorf("init_delay = false, want true")
	}
	if c.Padding1() != 0x55 {
		t.Errorf("padding1 = %X, want 0x55", c.Padding1())
	}
}

func TestConfigMaxBlockRange(t *testing.T) {
	c := NewConfig()
	if err := c.SetMaxBlock(MaxBlock + 1); !errors.Is(err, ErrOutOfRange) {
		t.Fatalf("err = %v, want ErrOutOfRange", err)
	}
}

func TestConfigSettersPreserveOtherBits(t *testing.T) {
	c := ConfigFromBits(0xFFFFFFFF)
	c.SetMasterKey(0)
	if c.Bits() != 0xFFFFFFF0 {
		t.Fatalf("bits = %08X, want FFFFFFF0", c.Bits())
	}
	c.SetAOR(false)
	if c.Bits()&(1<<22) != 0 {
		t.Fatalf("aor bit still set after SetAOR(false)")
	}
}

func TestConfigByteRoundTrip(t *testing.T) {
	c := FactoryDefault()
	raw := c.LEBytes()
	c2 := ConfigFromLEBytes(raw)
	if c.Bits() != c2.Bits() {
		t.Fatalf("LE round-trip lost data: %08X vs %08X", c.Bits(), c2.Bits())
	}
}
