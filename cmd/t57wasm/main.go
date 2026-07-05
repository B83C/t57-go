//go:build js

// Command t57wasm builds the T57 protocol library as a WebAssembly
// module that talks to a physical T57 reader via the browser's Web
// Serial API (navigator.serial).
//
// Build:
//
//	GOOS=js GOARCH=wasm go build -o web/dist/t57.wasm ./cmd/t57wasm/
//
// Then serve web/ and open index.html. See web/index.html for usage.
package main

import (
	"fmt"
	"syscall/js"

	"github.com/B83C/t57-go/internal/serial"
	"github.com/B83C/t57-go/internal/t57"
)

// Global state kept across JS calls.
var (
	tr     *serial.Transport
	client *t57.Client
)

// goMap returns a union of "ok" and "error" for a successful value.
func goMap(val interface{}) map[string]interface{} {
	return map[string]interface{}{"ok": val}
}

// errMap returns a union of "ok" and "error" for an error.
func errMap(err error) map[string]interface{} {
	return map[string]interface{}{"error": err.Error()}
}

// safe wraps a function so it returns {ok, error} maps instead of
// panicking on unexpected conditions.
func safe(fn func(this js.Value, args []js.Value) (interface{}, error)) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		result, err := fn(this, args)
		if err != nil {
			return errMap(err)
		}
		return goMap(result)
	})
}

// hexBytes encodes a byte slice as uppercase hex.
func hexBytes(b []byte) string {
	const digits = "0123456789ABCDEF"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = digits[v>>4]
		out[i*2+1] = digits[v&0x0F]
	}
	return string(out)
}

// mustClient returns the current Client or an error if not connected.
func mustClient() (*t57.Client, error) {
	if client == nil {
		return nil, fmt.Errorf("not connected — call t57Connect first")
	}
	return client, nil
}

func debugLog(msg string, args ...interface{}) {
	c := js.Global().Get("console")
	if c.Truthy() {
		c.Call("debug", fmt.Sprintf("t57.frame: "+msg, args...))
	}
}

func main() {
	// Wire frame-level debug logging to the browser console.
	t57.DebugLog = func(msg string, args ...interface{}) {
		debugLog(msg, args...)
	}

	js.Global().Set("t57Init", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		if err := serial.CheckAvailable(); err != nil {
			return nil, err
		}
		return "Web Serial API available", nil
	}))

	js.Global().Set("t57RequestPort", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		port, err := serial.RequestPort()
		if err != nil {
			return nil, err
		}
		info := port.Call("getInfo")
		return map[string]interface{}{
			"usbVendorId":  info.Get("usbVendorId").Int(),
			"usbProductId": info.Get("usbProductId").Int(),
		}, nil
	}))

	js.Global().Set("t57GetPorts", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		return serial.GetPorts()
	}))

	js.Global().Set("t57ConnectPort", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		if len(args) < 1 || !args[0].Truthy() {
			return nil, fmt.Errorf("no port provided")
		}
		port := args[0]
		baud := 9600
		if len(args) > 1 && args[1].Int() > 0 {
			baud = args[1].Int()
		}

		if tr != nil {
			tr.Close()
			tr = nil
			client = nil
		}

		js.Global().Get("console").Call("debug", "t57: opening port at", baud, "baud")
		t, err := serial.Open(port, baud)
		if err != nil {
			js.Global().Get("console").Call("error", "t57: open failed:", err.Error())
			return nil, err
		}
		c := t57.NewClient(t).WithRetries(0)
		js.Global().Get("console").Call("debug", "t57: port open, sending SysGetSerlNum")
		sn, err := c.SerialNumber()
		if err != nil {
			t.Close()
			js.Global().Get("console").Call("error", "t57: SysGetSerlNum failed:", err.Error())
			return nil, fmt.Errorf("T57 not responding at %d baud: %w", baud, err)
		}
		tr = t
		client = c
		snHex := hexBytes(sn[:])
		js.Global().Get("console").Call("debug", "t57: connected, serial:", snHex)
		return map[string]interface{}{
			"success": true,
			"baud":    baud,
			"serial":  snHex,
		}, nil
	}))

	// t57Connect is deprecated.
	js.Global().Set("t57Connect", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		return nil, fmt.Errorf("use t57ConnectPort (call navigator.serial.requestPort() from JS first)")
	}))



	js.Global().Set("t57Disconnect", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		if tr != nil {
			tr.Close()
			tr = nil
			client = nil
		}
		return "disconnected", nil
	}))

	js.Global().Set("t57ReadBlock", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		c, err := mustClient()
		if err != nil {
			return nil, err
		}
		block := uint8(args[0].Int())
		blk, err := c.ReadBlock(block)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"block": block,
			"hex":   hexBytes(blk[:]),
		}, nil
	}))

	js.Global().Set("t57ReadBlocks", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		c, err := mustClient()
		if err != nil {
			return nil, err
		}
		start := uint8(args[0].Int())
		count := uint8(args[1].Int())
		blks, err := c.ReadBlocks(start, count)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]interface{}, len(blks))
		for i, b := range blks {
			out[i] = map[string]interface{}{
				"block": int(start) + i,
				"hex":   hexBytes(b[:]),
			}
		}
		return out, nil
	}))

	js.Global().Set("t57WriteBlock", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		c, err := mustClient()
		if err != nil {
			return nil, err
		}
		block := uint8(args[0].Int())
		hex := args[1].String()
		blk, err := t57.ParseBlock(hex)
		if err != nil {
			return nil, fmt.Errorf("invalid block hex %q: %w", hex, err)
		}
		if err := c.WriteBlock(block, blk); err != nil {
			return nil, err
		}
		return fmt.Sprintf("wrote block %d", block), nil
	}))

	js.Global().Set("t57ReadAll", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		c, err := mustClient()
		if err != nil {
			return nil, err
		}
		js.Global().Get("console").Call("debug", "t57: reading all blocks (fast path via ReadAllRaw)")
		blocks, err := c.ReadAllRaw()
		if err != nil {
			js.Global().Get("console").Call("error", "t57: ReadAllRaw failed:", err.Error())
			// Fall back: read config + individual blocks.
			js.Global().Get("console").Call("debug", "t57: falling back to individual reads")
			cfg, e2 := c.ReadConfig()
			if e2 != nil {
				return nil, e2
			}
			blks, e2 := c.ReadBlocks(1, 7)
			if e2 != nil {
				return nil, e2
			}
			blocks[0] = cfg.LEBytes()
			for i, b := range blks {
				blocks[i+1] = b
			}
		}
		out := make([]map[string]interface{}, 8)
		for i, blk := range blocks {
			out[i] = map[string]interface{}{
				"block": i,
				"hex":   hexBytes(blk[:]),
			}
		}
		js.Global().Get("console").Call("debug", "t57: read all 8 blocks ok")
		return out, nil
	}))

	js.Global().Set("t57WriteBlocks", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		c, err := mustClient()
		if err != nil {
			return nil, err
		}
		blocks := args[0]
		length := blocks.Get("length").Int()
		if length == 0 {
			return nil, fmt.Errorf("no blocks to write")
		}
		written := 0
		for i := 0; i < length; i++ {
			item := blocks.Index(i)
			blockNum := uint8(item.Get("block").Int())
			hex := item.Get("hex").String()
			blk, err := t57.ParseBlock(hex)
			if err != nil {
				return nil, fmt.Errorf("block %d: %w", blockNum, err)
			}
			if blockNum == 0 {
				// Config block — use the typed setter.
				var raw [4]byte
				copy(raw[:], blk[:])
				if err := c.WriteConfig(t57.ConfigFromLEBytes(raw)); err != nil {
					return nil, fmt.Errorf("write config: %w", err)
				}
			} else {
				if err := c.WriteBlock(blockNum, blk); err != nil {
					return nil, fmt.Errorf("write block %d: %w", blockNum, err)
				}
			}
			written++
		}
		return fmt.Sprintf("wrote %d block(s)", written), nil
	}))

	js.Global().Set("t57ReadConfig", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		c, err := mustClient()
		if err != nil {
			return nil, err
		}
		cfg, err := c.ReadConfig()
		if err != nil {
			return nil, err
		}
		raw := cfg.LEBytes()
		return map[string]interface{}{
			"hex":          hexBytes(raw[:]),
			"u32":          cfg.Bits(),
			"masterKey":    cfg.MasterKey(),
			"dataBitRate":  cfg.DataBitRate().String(),
			"modulation":   cfg.Modulation().String(),
			"pskcf":        cfg.PskCf().String(),
			"aor":          cfg.AOR(),
			"maxBlock":     cfg.MaxBlock(),
			"pwd":          cfg.PWD(),
			"stTerminator": cfg.STSequenceTerminator(),
			"initDelay":    cfg.InitDelay(),
		}, nil
	}))

	js.Global().Set("t57WriteConfig", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		c, err := mustClient()
		if err != nil {
			return nil, err
		}
		hex := args[0].String()
		raw, err := t57.HexBytes(hex)
		if err != nil {
			return nil, fmt.Errorf("invalid config hex %q: %w", hex, err)
		}
		if len(raw) != 4 {
			return nil, fmt.Errorf("config must be 4 bytes, got %d", len(raw))
		}
		var b [4]byte
		copy(b[:], raw)
		cfg := t57.ConfigFromLEBytes(b)
		if err := c.WriteConfig(cfg); err != nil {
			return nil, err
		}
		return "wrote config", nil
	}))

	js.Global().Set("t57ConfigDefault", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		c, err := mustClient()
		if err != nil {
			return nil, err
		}
		if err := c.WriteConfig(t57.FactoryDefault()); err != nil {
			return nil, err
		}
		return "wrote default config", nil
	}))

	js.Global().Set("t57SerialNumber", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		c, err := mustClient()
		if err != nil {
			return nil, err
		}
		sn, err := c.SerialNumber()
		if err != nil {
			return nil, err
		}
		return hexBytes(sn[:]), nil
	}))

	js.Global().Set("t57Version", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		c, err := mustClient()
		if err != nil {
			return nil, err
		}
		v, err := c.FirmwareVersion()
		if err != nil {
			return nil, err
		}
		return string(v), nil
	}))

	js.Global().Set("t57Buzzer", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		c, err := mustClient()
		if err != nil {
			return nil, err
		}
		on := uint8(args[0].Int())
		cyc := uint8(args[1].Int())
		return nil, c.Buzzer(on, cyc)
	}))

	js.Global().Set("t57Led", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		c, err := mustClient()
		if err != nil {
			return nil, err
		}
		on := uint8(args[0].Int())
		cyc := uint8(args[1].Int())
		return nil, c.LED(on, cyc)
	}))

	js.Global().Set("t57Raw", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		c, err := mustClient()
		if err != nil {
			return nil, err
		}
		cmd := uint8(args[0].Int())
		dataHex := args[1].String()
		var data []byte
		if dataHex != "" {
			data, err = t57.HexBytes(dataHex)
			if err != nil {
				return nil, fmt.Errorf("invalid data hex: %w", err)
			}
		}
		raw, err := c.Transact(t57.Command(cmd), data)
		if err != nil {
			return nil, err
		}
		return hexBytes(raw), nil
	}))

	js.Global().Set("t57SetBaud", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		c, err := mustClient()
		if err != nil {
			return nil, err
		}
		rate := uint8(args[0].Int())
		code, ok := t57.ParseBaud(int(rate))
		if !ok {
			return nil, fmt.Errorf("unsupported rate: %d", rate)
		}
		if err := c.SetBaud(code); err != nil {
			return nil, err
		}
		return fmt.Sprintf("baud change to %d requested (power-cycle to take effect)", rate), nil
	}))

	js.Global().Set("t57SetAddress", safe(func(this js.Value, args []js.Value) (interface{}, error) {
		c, err := mustClient()
		if err != nil {
			return nil, err
		}
		addr := uint8(args[0].Int())
		if err := c.SetAddress(addr); err != nil {
			return nil, err
		}
		return fmt.Sprintf("address set to 0x%02X", addr), nil
	}))

	// Block forever so the WASM module stays alive.
	select {}
}
