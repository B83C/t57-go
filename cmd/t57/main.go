// Command t57 reads and writes T57 RFID tags over a serial reader.
//
// See `t57 help` for the full command list.
package main

import (
	"bufio"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/B83C/t57-go/internal/serial"
	"github.com/B83C/t57-go/internal/t57"
)

//go:embed web/index.html web/dist/t57.wasm web/dist/wasm_exec.js
var webFS embed.FS

// webRoot serves the embedded files at "/" by stripping the "web/" prefix
// from the embedded paths.
func webRoot() http.FileSystem {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	return http.FS(sub)
}

// args is the top-level argument struct parsed from os.Args.
type args struct {
	Port     string  `arg:"-p, --port" default:"auto" help:"serial port (auto-detect if 'auto')"`
	Baud     int     `arg:"--baud"      default:"115200" help:"default baud rate for auto-detect"`
	Retries  int     `arg:"--retries"   default:"3"     help:"retry transient errors N times"`
	Yes      bool    `arg:"-y, --yes"                 help:"answer yes to confirmation prompts"`
	Verbose  bool    `arg:"-v, --verbose"             help:"enable verbose output"`
	JSON     bool    `arg:"--json"                     help:"machine-readable JSON output"`
	NoHist   bool    `arg:"--no-history"               help:"do not save reads to the history file"`
	HistPath string  `arg:"--history"                  help:"override history file path"`
	Listen   string  `arg:"-l, --listen"               help:"address:port for 'serve' command (default :8080)"`
	Command  command `arg:"positional"`
}

// command groups the subcommands. Using an interface-style union to keep
// the dispatch table-driven.
type command interface {
	isCommand()
}

type cmdRead struct {
	Start  int8  `arg:"-s, --start"  default:"-1"  help:"first block to read (1-based)"`
	Count  uint8 `arg:"-c, --count"  default:"6"   help:"number of blocks to read"`
	Block  int8  `arg:"-b, --block"  default:"-1"  help:"single block to read (0=config, 1..=7=user)"`
	Page   uint8 `arg:"--page"       default:"0"   help:"T5557 page (0 or 1)"`
	Config bool  `arg:"--config"                   help:"read the config block (block 0, page 0)"`
}
type cmdWrite struct {
	Block  int8    `arg:"-b, --block"  default:"-1"  help:"block to write to (0=config, 1..=7=user)"`
	Page   uint8   `arg:"--page"       default:"0"   help:"T5557 page (0 or 1)"`
	Values []string `arg:"-w, --write"  help:"hex values to write, comma-separated"`
	Force  bool    `arg:"-f, --force"                help:"allow writing zero-filled blocks without --yes"`
}
type cmdRaw struct {
	Station uint8  `arg:"--station" default:"0"  help:"station address byte"`
	Command uint8  `arg:"-c, --command"           help:"command byte (0x00..=0xFF)"`
	Data    string `arg:"-d, --data"  default:"" help:"payload as hex"`
}
type cmdDuplicate struct{}
type cmdConfigRead struct{}
type cmdConfigWrite struct {
	Value string `arg:"positional" help:"32-bit config value as hex (0x...)"`
}
type cmdConfigDefault struct{}
type cmdBaud struct {
	Rate int `arg:"positional" help:"target baud rate: 9600, 19200, 38400, 57600, 115200"`
}
type cmdAddress struct {
	Addr uint8 `arg:"positional" help:"new station address (0..=255)"`
}
type cmdBuzzer struct {
	On     uint8 `arg:"--on"     default:"5"  help:"on-time units (1 unit = 20ms)"`
	Cycles uint8 `arg:"--cycles" default:"1"  help:"number of on/off cycles"`
}
type cmdLED struct {
	On     uint8 `arg:"--on"     default:"5"  help:"on-time units (1 unit = 20ms)"`
	Cycles uint8 `arg:"--cycles" default:"1"  help:"number of on/off cycles"`
}
type cmdVersion struct{}
type cmdScan struct{}
type cmdPorts struct{}
type cmdHistoryList struct {
	Limit int `arg:"--limit" default:"20" help:"max entries to show"`
}
type cmdHistoryShow struct {
	Index int `arg:"positional" help:"1-based index"`
}
type cmdHistoryLast struct{}
type cmdHistoryClear struct{}
type cmdHistoryPath struct{}

func (cmdRead) isCommand()             {}
func (cmdWrite) isCommand()            {}
func (cmdRaw) isCommand()              {}
func (cmdDuplicate) isCommand()        {}
func (cmdConfigRead) isCommand()       {}
func (cmdConfigWrite) isCommand()      {}
func (cmdConfigDefault) isCommand()    {}
func (cmdBaud) isCommand()             {}
func (cmdAddress) isCommand()          {}
func (cmdBuzzer) isCommand()           {}
func (cmdLED) isCommand()              {}
func (cmdVersion) isCommand()          {}
func (cmdScan) isCommand()             {}
func (cmdPorts) isCommand()            {}
func (cmdHistoryList) isCommand()      {}
func (cmdHistoryShow) isCommand()      {}
func (cmdHistoryLast) isCommand()      {}
func (cmdHistoryClear) isCommand()     {}
func (cmdHistoryPath) isCommand()      {}
type cmdServe struct {
	Addr string
}
func (cmdServe) isCommand() {}
type cmdTUI struct{}
func (cmdTUI) isCommand() {}

func main() {
	// We do our own simple argv parser to avoid pulling in a big
	// dependency. This is a CLI for a single-purpose tool; we don't
	// need subcommand help, just enough to be ergonomic.
	cli, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	if err := run(cli); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run is the top-level dispatcher. Every command funnels through here
// so a single err check at the bottom of main handles everything.
func run(c *args) error {
	// Set up the history file.
	histPath := c.HistPath
	if histPath == "" {
		histPath = t57.DefaultHistoryPath()
	}
	var hist *t57.History
	if !c.NoHist && histPath != "" {
		hist = t57.OpenHistory(histPath)
	}

	switch cmd := c.Command.(type) {
	case cmdScan:
		return cmdScanImpl(c)
	case cmdPorts:
		return cmdPortsImpl(c)
	case cmdRead:
		return runRead(c, hist, cmd)
	case cmdWrite:
		return runWrite(c, hist, cmd)
	case cmdRaw:
		return runRaw(c, cmd)
	case cmdDuplicate:
		return runDuplicate(c, hist)
	case cmdConfigRead:
		return runConfigRead(c)
	case cmdConfigWrite:
		return runConfigWrite(c, cmd)
	case cmdConfigDefault:
		return runConfigDefault(c)
	case cmdBaud:
		return runBaud(c, cmd)
	case cmdAddress:
		return runAddress(c, cmd)
	case cmdBuzzer:
		return runBuzzer(c, cmd)
	case cmdLED:
		return runLED(c, cmd)
	case cmdVersion:
		return runVersion(c)
	case cmdHistoryList:
		return cmdHistoryListImpl(c, hist, cmd)
	case cmdHistoryShow:
		return cmdHistoryShowImpl(c, hist, cmd)
	case cmdHistoryLast:
		return cmdHistoryLastImpl(c, hist)
	case cmdHistoryClear:
		return cmdHistoryClearImpl(c, hist)
	case cmdHistoryPath:
		return cmdHistoryPathImpl(hist)
	case cmdServe:
		if c.Listen != "" {
			cmd.Addr = c.Listen
		}
		return runServe(cmd)
	case cmdTUI:
		return runTUI(c)
	}
	return fmt.Errorf("unknown command")
}

// openClient opens the configured port and returns a connected client.
func openClient(c *args) (*t57.Client, error) {
	if c.Port == "auto" || c.Port == "" {
		return autoDetect(c)
	}
	return openExplicit(c.Port, c.Baud, c.Retries)
}

func openExplicit(path string, baud, retries int) (*t57.Client, error) {
	tr, err := serial.Open(path, baud)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	return t57.NewClient(tr).WithRetries(retries), nil
}

func autoDetect(c *args) (*t57.Client, error) {
	ports, err := serial.ListPorts()
	if err != nil {
		return nil, fmt.Errorf("listing serial ports: %w", err)
	}
	if len(ports) == 0 {
		return nil, fmt.Errorf("no serial ports found")
	}

	// Baud rates to probe, in the same order the Zig code implies:
	// try the configured baud first, then fall through the spec list.
	bauds := []int{c.Baud}
	for _, r := range []int{9600, 19200, 38400, 57600, 115200} {
		if r != c.Baud {
			bauds = append(bauds, r)
		}
	}

	var lastErr error
	for _, p := range ports {
		if c.Verbose {
			fmt.Fprintf(os.Stderr, "probing %s ...\n", p.Name)
		}
		for _, baud := range bauds {
			tr, err := serial.OpenWithTimeout(p.Name, baud, 300*time.Millisecond)
			if err != nil {
				if c.Verbose {
					fmt.Fprintf(os.Stderr, "  %s @ %d: open failed: %v\n", p.Name, baud, err)
				}
				lastErr = err
				continue
			}
			client := t57.NewClient(tr).WithRetries(0)
			sn, err := client.SerialNumber()
			if err == nil {
				if c.Verbose {
					fmt.Fprintf(os.Stderr, "  ✓ %s @ %d baud, serial=%X\n", p.Name, baud, sn)
				}
				return client.WithRetries(c.Retries), nil
			}
			tr.Close()
			if c.Verbose {
				fmt.Fprintf(os.Stderr, "  — %s @ %d baud: %v\n", p.Name, baud, err)
			}
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("no T57 device found on %d port(s); last attempt: %v", len(ports), lastErr)
	}
	return nil, fmt.Errorf("no T57 device found on %d port(s)", len(ports))
}

func cmdScanImpl(c *args) error {
	ports, err := serial.ListPorts()
	if err != nil {
		return err
	}
	if len(ports) == 0 {
		fmt.Println("(no serial ports found)")
		return nil
	}
	bauds := []int{9600, 19200, 38400, 57600, 115200}
	for _, p := range ports {
		found := false
		for _, baud := range bauds {
			tr, err := serial.OpenWithTimeout(p.Name, baud, 300*time.Millisecond)
			if err != nil {
				if c.Verbose {
					fmt.Printf("%s @ %d baud: open failed (%v)\n", p.Name, baud, err)
				}
				continue
			}
			client := t57.NewClient(tr).WithRetries(0)
			sn, err := client.SerialNumber()
			if err == nil {
				hex := t57.FormatHex(sn[:])
				fmt.Printf("%s @ %d baud: T57 detected, serial %s\n", p.Name, baud, hex)
				tr.Close()
				found = true
				break
			}
			if c.Verbose {
				fmt.Printf("%s @ %d baud: not a T57 (%v)\n", p.Name, baud, err)
			}
			tr.Close()
		}
		if !found && c.Verbose {
			fmt.Printf("%s: no T57 found at any baud rate\n", p.Name)
		}
	}
	return nil
}

func cmdPortsImpl(c *args) error {
	ports, err := serial.ListPorts()
	if err != nil {
		return err
	}
	for _, p := range ports {
		fmt.Println(p.Name)
	}
	if c.Verbose {
		fmt.Fprintf(os.Stderr, "(scanned at baud %d)\n", c.Baud)
	}
	return nil
}

func runRead(c *args, hist *t57.History, cmd cmdRead) error {
	client, err := openClient(c)
	if err != nil {
		return err
	}
	defer client.Close()

	if cmd.Config {
		cfg, err := client.ReadConfig()
		if err != nil {
			return err
		}
		return printConfig(c, cfg, "config")
	}

	if cmd.Block >= 0 {
		blk, err := client.ReadBlockRaw(t57.Page(cmd.Page), uint8(cmd.Block))
		if err != nil {
			return err
		}
		if hist != nil {
			_ = hist.Add(portName(c), "read",
				fmt.Sprintf("page=%d block=%d", cmd.Page, cmd.Block), blk[:])
		}
		return printBlock(c, blk, fmt.Sprintf("block %d", cmd.Block))
	}

	start := uint8(1)
	if cmd.Start >= 0 {
		start = uint8(cmd.Start)
	}
	blks, err := client.ReadBlocks(start, cmd.Count)
	if err != nil {
		return err
	}
	for i, b := range blks {
		n := int(start) + i
		if hist != nil {
			_ = hist.Add(portName(c), "read",
				fmt.Sprintf("page=0 block=%d", n), b[:])
		}
		if err := printBlock(c, b, fmt.Sprintf("block %d", n)); err != nil {
			return err
		}
	}
	return nil
}

func runWrite(c *args, hist *t57.History, cmd cmdWrite) error {
	if len(cmd.Values) == 0 {
		return fmt.Errorf("no values provided (use --force to write zeros)")
	}
	client, err := openClient(c)
	if err != nil {
		return err
	}
	defer client.Close()

	for i, vs := range cmd.Values {
		blk, err := t57.ParseBlock(vs)
		if err != nil {
			return fmt.Errorf("value %d: %w", i, err)
		}
		blockNum := uint8(i + 1)
		if cmd.Block >= 0 {
			blockNum = uint8(cmd.Block)
		}
		if !cmd.Force && isAllZero(blk) {
			return fmt.Errorf("refusing to write all-zeros to block %d without --force", blockNum)
		}
		if !c.Yes {
			if !confirm(fmt.Sprintf("about to write %s to block %d; continue? [y/N] ",
				t57.FormatBlock(blk), blockNum)) {
				return fmt.Errorf("aborted by user")
			}
		}
		if err := client.WriteBlock(blockNum, blk); err != nil {
			return fmt.Errorf("write block %d: %w", blockNum, err)
		}
		fmt.Printf("wrote %s to block %d\n", t57.FormatBlock(blk), blockNum)
		if hist != nil {
			_ = hist.Add(portName(c), "write",
				fmt.Sprintf("page=%d block=%d", cmd.Page, blockNum), blk[:])
		}
	}
	return nil
}

func runRaw(c *args, cmd cmdRaw) error {
	client, err := openClient(c)
	if err != nil {
		return err
	}
	defer client.Close()

	var data []byte
	if cmd.Data != "" {
		data, err = t57.HexBytes(cmd.Data)
		if err != nil {
			return err
		}
	}
	raw, err := client.Transact(t57.Command(cmd.Command), data)
	if err != nil {
		return err
	}
	if c.JSON {
		out := struct {
			Hex   string `json:"hex"`
			Bytes []byte `json:"bytes"`
		}{Hex: "0x" + t57.FormatHex(raw), Bytes: raw}
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	fmt.Println("0x" + t57.FormatHex(raw))
	return nil
}

func runDuplicate(c *args, hist *t57.History) error {
	client, err := openClient(c)
	if err != nil {
		return err
	}
	defer client.Close()

	if !c.Yes {
		if !confirm("Ready to read from the old card? [Y/n] ") {
			return fmt.Errorf("aborted by user")
		}
	}
	fmt.Println("Reading 6 user blocks...")
	blks, err := client.ReadBlocks(1, 6)
	if err != nil {
		return err
	}
	for i, b := range blks {
		if hist != nil {
			_ = hist.Add(portName(c), "duplicate-read",
				fmt.Sprintf("block %d", i+1), b[:])
		}
		fmt.Printf("read block %d = %s\n", i+1, t57.FormatBlock(b))
	}
	if !c.Yes {
		if !confirm("\nReady to copy onto a new card? [Y/n] ") {
			return fmt.Errorf("aborted by user")
		}
	}
	for i, b := range blks {
		if err := client.WriteBlock(uint8(i+1), b); err != nil {
			return fmt.Errorf("write block %d: %w", i+1, err)
		}
		fmt.Printf("wrote block %d\n", i+1)
	}
	return nil
}

func runConfigRead(c *args) error {
	client, err := openClient(c)
	if err != nil {
		return err
	}
	defer client.Close()
	cfg, err := client.ReadConfig()
	if err != nil {
		return err
	}
	return printConfig(c, cfg, "config")
}

func runConfigWrite(c *args, cmd cmdConfigWrite) error {
	raw, err := t57.HexBytes(cmd.Value)
	if err != nil {
		return err
	}
	if len(raw) != 4 {
		return fmt.Errorf("expected 4 bytes, got %d", len(raw))
	}
	var b [4]byte
	copy(b[:], raw)
	cfg := t57.ConfigFromBEBytes(b)
	client, err := openClient(c)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.WriteConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("wrote config %s (%s)\n", "0x"+t57.FormatHex(raw), cfg)
	return nil
}

func runConfigDefault(c *args) error {
	client, err := openClient(c)
	if err != nil {
		return err
	}
	defer client.Close()
	cfg := t57.FactoryDefault()
	if err := client.WriteConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("wrote default config %s\n", cfg)
	return nil
}

func runBaud(c *args, cmd cmdBaud) error {
	code, ok := t57.ParseBaud(cmd.Rate)
	if !ok {
		return fmt.Errorf("unsupported baud rate: %d", cmd.Rate)
	}
	client, err := openClient(c)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.SetBaud(code); err != nil {
		return err
	}
	fmt.Printf("requested baud change to %d; power-cycle the device to take effect\n", code.Baud())
	return nil
}

func runAddress(c *args, cmd cmdAddress) error {
	client, err := openClient(c)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.SetAddress(cmd.Addr); err != nil {
		return err
	}
	fmt.Printf("set device address to 0x%02X\n", cmd.Addr)
	return nil
}

func runBuzzer(c *args, cmd cmdBuzzer) error {
	client, err := openClient(c)
	if err != nil {
		return err
	}
	defer client.Close()
	return client.Buzzer(cmd.On, cmd.Cycles)
}

func runLED(c *args, cmd cmdLED) error {
	client, err := openClient(c)
	if err != nil {
		return err
	}
	defer client.Close()
	return client.LED(cmd.On, cmd.Cycles)
}

func runVersion(c *args) error {
	client, err := openClient(c)
	if err != nil {
		return err
	}
	defer client.Close()
	raw, err := client.FirmwareVersion()
	if err != nil {
		return err
	}
	s := strings.TrimRight(string(raw), "\x00")
	fmt.Println(s)
	return nil
}

func cmdHistoryListImpl(c *args, h *t57.History, cmd cmdHistoryList) error {
	es := h.Entries()
	n := cmd.Limit
	if n > len(es) {
		n = len(es)
	}
	for i := 0; i < n; i++ {
		e := es[len(es)-1-i] // newest first
		if c.JSON {
			_ = json.NewEncoder(os.Stdout).Encode(e)
		} else {
			fmt.Println(e)
		}
	}
	return nil
}

func cmdHistoryShowImpl(c *args, h *t57.History, cmd cmdHistoryShow) error {
	es := h.Entries()
	if cmd.Index < 1 || cmd.Index > len(es) {
		return fmt.Errorf("index %d out of range 1..=%d", cmd.Index, len(es))
	}
	e := es[cmd.Index-1]
	if c.JSON {
		return json.NewEncoder(os.Stdout).Encode(e)
	}
	fmt.Println(e)
	return nil
}

func cmdHistoryLastImpl(c *args, h *t57.History) error {
	e, ok := h.Last()
	if !ok {
		fmt.Println("(no history yet)")
		return nil
	}
	if c.JSON {
		return json.NewEncoder(os.Stdout).Encode(e)
	}
	fmt.Println(e)
	return nil
}

func cmdHistoryClearImpl(c *args, h *t57.History) error {
	return h.Clear()
}

func cmdHistoryPathImpl(h *t57.History) error {
	if h == nil {
		fmt.Println("(history disabled)")
		return nil
	}
	fmt.Println(h.Path())
	return nil
}

// --- printing helpers ---

func printBlock(c *args, b [t57.BlockSize]byte, label string) error {
	if c.JSON {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Label string `json:"label"`
			Hex   string `json:"hex"`
			Bytes []byte `json:"bytes"`
		}{label, "0x" + t57.FormatHex(b[:]), b[:]})
	}
	fmt.Printf("%s: 0x%s\n", label, t57.FormatHex(b[:]))
	return nil
}

func printConfig(c *args, cfg t57.Config, label string) error {
	raw := cfg.LEBytes()
	if c.JSON {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Label        string `json:"label"`
			Hex          string `json:"hex"`
			U32LE        uint32 `json:"u32_le"`
			MasterKey    uint8  `json:"master_key"`
			DataBitRate  string `json:"data_bit_rate"`
			Modulation   string `json:"modulation"`
			PskCf        string `json:"pskcf"`
			AOR          bool   `json:"aor"`
			MaxBlock     uint8  `json:"max_block"`
			PWD          bool   `json:"pwd"`
			ST           bool   `json:"st_sequence_terminator"`
			InitDelay    bool   `json:"init_delay"`
			Padding1     uint8  `json:"padding1"`
		}{
			Label:        label,
			Hex:          "0x" + t57.FormatHex(raw[:]),
			U32LE:        cfg.Bits(),
			MasterKey:    cfg.MasterKey(),
			DataBitRate:  cfg.DataBitRate().String(),
			Modulation:   cfg.Modulation().String(),
			PskCf:        cfg.PskCf().String(),
			AOR:          cfg.AOR(),
			MaxBlock:     cfg.MaxBlock(),
			PWD:          cfg.PWD(),
			ST:           cfg.STSequenceTerminator(),
			InitDelay:    cfg.InitDelay(),
			Padding1:     cfg.Padding1(),
		})
	}
	fmt.Printf("%s: 0x%s (%s)\n", label, t57.FormatHex(raw[:]), cfg)
	return nil
}

func portName(c *args) string {
	if c.Port == "auto" {
		return ""
	}
	return c.Port
}

func isAllZero(b [t57.BlockSize]byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

// confirm prompts the user on stderr and returns true if they
// answered 'y' or 'Y' (or just pressed enter, if `emptyYes`).
func confirm(prompt string) bool {
	fmt.Fprint(os.Stderr, prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	ans := strings.TrimSpace(line)
	if ans == "" {
		ans = "y"
	}
	ans = strings.ToLower(ans)
	return ans == "y" || ans == "yes"
}

// --- minimal arg parser ---

// parseArgs is a hand-rolled, just-good-enough argument parser. It
// supports `--flag value`, `--flag=value`, `-f value`, `-fvalue`, and
// `--` to terminate flag parsing.
func parseArgs(argv []string) (*args, error) {
	a := &args{
		Baud:    115200,
		Retries: 3,
		Port:    "auto",
	}
	positional := []string{}
	i := 0
	for i < len(argv) {
		s := argv[i]
		if s == "--" {
			positional = append(positional, argv[i+1:]...)
			break
		}
		name, val, hasVal := splitFlag(s)
		if !strings.HasPrefix(s, "-") || name == "" {
			positional = append(positional, s)
			i++
			continue
		}
		// This is a flag.  Determine its value.
		flagVal := val
		eatNext := false
		if !hasVal && flagTakesValue(name) && i+1 < len(argv) {
			flagVal = argv[i+1]
			eatNext = true
		}
		if applyFlag(a, name, flagVal) {
			// Known global — skip its value arg if we ate it.
			i++
			if eatNext {
				i++
			}
			continue
		}
		// Unknown flag — pass through as-is, including its value.
		positional = append(positional, s)
		if eatNext {
			positional = append(positional, flagVal)
			i += 2
		} else if hasVal {
			positional = append(positional, val)
			i++
		} else {
			i++
		}
	}
	cmd, err := parseCommand(positional)
	if err != nil {
		return nil, err
	}
	a.Command = cmd
	return a, nil
}

func splitFlag(s string) (name, val string, hasVal bool) {
	if strings.HasPrefix(s, "--") {
		s = s[2:]
	} else {
		s = s[1:]
	}
	if eq := strings.IndexByte(s, '='); eq >= 0 {
		return s[:eq], s[eq+1:], true
	}
	return s, "", false
}

func flagTakesValue(name string) bool {
	switch name {
	case "p", "port", "baud", "retries", "history", "s", "start",
		"c", "count", "b", "block", "page", "w", "write",
		"d", "data", "command", "station", "limit", "value",
		"on", "cycles", "l", "listen":
		return true
	}
	return false
}

func applyFlag(a *args, name, val string) bool {
	switch name {
	case "p", "port":
		a.Port = val
		return true
	case "baud":
		n, err := strconv.Atoi(val)
		if err != nil {
			return false
		}
		a.Baud = n
		return true
	case "retries":
		n, err := strconv.Atoi(val)
		if err != nil {
			return false
		}
		a.Retries = n
		return true
	case "y", "yes":
		a.Yes = true
		return true
	case "v", "verbose":
		a.Verbose = true
		return true
	case "json":
		a.JSON = true
		return true
	case "no-history":
		a.NoHist = true
		return true
	case "history":
		a.HistPath = val
		return true
	case "l", "listen":
		a.Listen = val
		return true
	}
	return false
}

func parseCommand(args []string) (command, error) {
	if len(args) == 0 {
		return nil, printUsage()
	}
	name := args[0]
	rest := args[1:]
	switch name {
	case "scan":
		return cmdScan{}, nil
	case "ports":
		return cmdPorts{}, nil
	case "read":
		return parseRead(rest)
	case "write":
		return parseWrite(rest)
	case "raw":
		return parseRaw(rest)
	case "duplicate":
		return cmdDuplicate{}, nil
	case "config":
		return parseConfig(rest)
	case "baud":
		return parseBaud(rest)
	case "address":
		return parseAddress(rest)
	case "buzzer":
		return parseBuzzer(rest)
	case "led":
		return parseLED(rest)
	case "version":
		return cmdVersion{}, nil
	case "serve":
		return cmdServe{Addr: ":8080"}, nil
	case "tui", "interactive":
		return cmdTUI{}, nil
	case "history":
		return parseHistory(rest)
	case "help", "-h", "--help":
		return nil, printUsage()
	}
	return nil, fmt.Errorf("unknown command: %s", name)
}

func parseRead(args []string) (cmdRead, error) {
	c := cmdRead{Start: -1, Block: -1, Count: 6, Page: 0}
	if err := applyReadWrite(&c, args, "read", readSetter{c: &c}); err != nil {
		return c, err
	}
	return c, nil
}

type readSetter struct{ c *cmdRead }

func (s readSetter) setStart(n int)  { s.c.Start = int8(n) }
func (s readSetter) setCount(n int)  { s.c.Count = uint8(n) }
func (s readSetter) setBlock(n int)  { s.c.Block = int8(n) }
func (s readSetter) setPage(n int)   { s.c.Page = uint8(n) }
func (s readSetter) setConfig(b bool) { s.c.Config = b }
func (s readSetter) setForce(_ bool) {}
func (s readSetter) setValues(_ []string) {}

type writeSetter struct{ c *cmdWrite }

func (s writeSetter) setStart(_ int)  {}
func (s writeSetter) setCount(_ int)  {}
func (s writeSetter) setBlock(n int)  { s.c.Block = int8(n) }
func (s writeSetter) setPage(n int)   { s.c.Page = uint8(n) }
func (s writeSetter) setConfig(_ bool) {}
func (s writeSetter) setForce(b bool)  { s.c.Force = b }
func (s writeSetter) setValues(v []string) { s.c.Values = v }

func parseWrite(args []string) (cmdWrite, error) {
	c := cmdWrite{Block: -1, Page: 0}
	if err := applyReadWrite(&c, args, "write", writeSetter{&c}); err != nil {
		return c, err
	}
	return c, nil
}

type readWriteFlags interface {
	setStart(int)
	setCount(int)
	setBlock(int)
	setPage(int)
	setConfig(bool)
	setForce(bool)
	setValues([]string)
}

func applyReadWrite(c interface{}, args []string, kind string, s readWriteFlags) error {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			return fmt.Errorf("%s: unexpected positional argument: %s", kind, arg)
		}
		name, val, hasVal := splitFlag(arg)
		var value string
		if hasVal {
			value = val
		} else if i+1 < len(args) {
			value = args[i+1]
			i++
		} else {
			value = ""
		}
		switch name {
		case "s", "start":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("start: %w", err)
			}
			s.setStart(n)
		case "c", "count", "command":
			// "c" doubles as --command in raw mode.
			if kind == "raw" {
				_, err := strconv.ParseUint(value, 0, 16)
				if err != nil {
					return fmt.Errorf("command: %w", err)
				}
			} else {
				n, err := strconv.Atoi(value)
				if err != nil {
					return fmt.Errorf("count: %w", err)
				}
				s.setCount(n)
			}
		case "b", "block":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("block: %w", err)
			}
			s.setBlock(n)
		case "page":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("page: %w", err)
			}
			s.setPage(n)
		case "config":
			s.setConfig(true)
		case "f", "force":
			s.setForce(true)
		case "w", "write":
			vs := strings.Split(value, ",")
			s.setValues(vs)
		}
	}
	return nil
}

func parseRaw(args []string) (cmdRaw, error) {
	c := cmdRaw{Station: 0}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			return c, fmt.Errorf("raw: unexpected positional: %s", arg)
		}
		name, val, hasVal := splitFlag(arg)
		var value string
		if hasVal {
			value = val
		} else if i+1 < len(args) {
			value = args[i+1]
			i++
		} else {
			value = ""
		}
		switch name {
		case "station":
			n, err := strconv.ParseUint(value, 0, 8)
			if err != nil {
				return c, fmt.Errorf("station: %w", err)
			}
			c.Station = uint8(n)
		case "c", "command":
			n, err := strconv.ParseUint(value, 16, 8)
			if err != nil {
				return c, fmt.Errorf("command (hex): %w", err)
			}
			c.Command = uint8(n)
		case "d", "data":
			c.Data = value
		}
	}
	return c, nil
}

func parseConfig(args []string) (command, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("config: subcommand required (read, write, default)")
	}
	switch args[0] {
	case "read":
		return cmdConfigRead{}, nil
	case "write":
		if len(args) < 2 {
			return nil, fmt.Errorf("config write: requires a value")
		}
		return cmdConfigWrite{Value: args[1]}, nil
	case "default":
		return cmdConfigDefault{}, nil
	}
	return nil, fmt.Errorf("config: unknown subcommand: %s", args[0])
}

func parseBaud(args []string) (cmdBaud, error) {
	if len(args) != 1 {
		return cmdBaud{}, fmt.Errorf("baud: expected one rate (9600/19200/38400/57600/115200)")
	}
	n, err := strconv.Atoi(args[0])
	if err != nil {
		return cmdBaud{}, fmt.Errorf("baud: %w", err)
	}
	return cmdBaud{Rate: n}, nil
}

func parseAddress(args []string) (cmdAddress, error) {
	if len(args) != 1 {
		return cmdAddress{}, fmt.Errorf("address: expected one address byte")
	}
	n, err := strconv.ParseUint(args[0], 0, 8)
	if err != nil {
		return cmdAddress{}, fmt.Errorf("address: %w", err)
	}
	return cmdAddress{Addr: uint8(n)}, nil
}

func parseBuzzer(args []string) (cmdBuzzer, error) {
	c := cmdBuzzer{On: 5, Cycles: 1}
	for i := 0; i < len(args); i++ {
		name, val, hasVal := splitFlag(args[i])
		var value string
		if hasVal {
			value = val
		} else if i+1 < len(args) {
			value = args[i+1]
			i++
		} else {
			value = ""
		}
		switch name {
		case "on":
			n, _ := strconv.Atoi(value)
			c.On = uint8(n)
		case "cycles":
			n, _ := strconv.Atoi(value)
			c.Cycles = uint8(n)
		}
	}
	return c, nil
}

func parseLED(args []string) (cmdLED, error) {
	c := cmdLED{On: 5, Cycles: 1}
	for i := 0; i < len(args); i++ {
		name, val, hasVal := splitFlag(args[i])
		var value string
		if hasVal {
			value = val
		} else if i+1 < len(args) {
			value = args[i+1]
			i++
		} else {
			value = ""
		}
		switch name {
		case "on":
			n, _ := strconv.Atoi(value)
			c.On = uint8(n)
		case "cycles":
			n, _ := strconv.Atoi(value)
			c.Cycles = uint8(n)
		}
	}
	return c, nil
}

func parseHistory(args []string) (command, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("history: subcommand required (list, show, last, clear, path)")
	}
	switch args[0] {
	case "list":
		c := cmdHistoryList{Limit: 20}
		for i := 1; i < len(args); i++ {
			name, val, hasVal := splitFlag(args[i])
			if name == "limit" {
				if hasVal {
					c.Limit, _ = strconv.Atoi(val)
				} else if i+1 < len(args) {
					c.Limit, _ = strconv.Atoi(args[i+1])
					i++
				}
			}
		}
		return c, nil
	case "show":
		if len(args) < 2 {
			return nil, fmt.Errorf("history show: requires an index")
		}
		n, err := strconv.Atoi(args[1])
		if err != nil {
			return nil, fmt.Errorf("history show: %w", err)
		}
		return cmdHistoryShow{Index: n}, nil
	case "last":
		return cmdHistoryLast{}, nil
	case "clear":
		return cmdHistoryClear{}, nil
	case "path":
		return cmdHistoryPath{}, nil
	}
	return nil, fmt.Errorf("history: unknown subcommand: %s", args[0])
}

func printUsage() error {
	fmt.Fprintln(os.Stderr, `Usage: t57 [global-opts] <command> [args]

Global options:
  -p, --port <PATH>     serial port (default: auto-detect)
      --baud <RATE>     default baud rate (default: 115200)
      --retries <N>     retry transient errors (default: 3)
  -y, --yes             answer yes to all confirmation prompts
  -v, --verbose         verbose output
      --json            machine-readable JSON output
      --no-history      do not save reads to the history file
      --history <PATH>  override history file path
  -l, --listen :8080    listen address for 'serve' command

Commands:
  scan                                  probe every serial port for a T57
  ports                                 list serial ports on the host
  read [-b N] [--config] [-s N -c N]    read blocks
  write [-b N] -w <VAL,VAL,...> [-f]    write blocks
  raw --command <HEX> [-d <HEX>]        send a raw command, print response
  duplicate                             read tag, then write to another tag
  config {read|write <HEX>|default}     read/write the config block (block 0)
  baud <RATE>                           change device baud rate
  address <0..=255>                     change device station address
  buzzer [--on N --cycles N]            beep the buzzer
  led [--on N --cycles N]               blink the LED
  version                               print firmware version
  history {list|show N|last|clear|path} manage read/write history
  serve                                 start web UI with Web Serial API
  tui                                   interactive hex editor in the terminal
  help                                  print this message

Run 't57 help' to see this message.`)
	os.Exit(0)
	return nil
}

// handleSigint installs a Ctrl-C handler that flushes output and exits
// cleanly. Without it, the CLI leaves the serial port in a half-open
// state on interruption.
// runServe starts a local HTTP server that serves the embedded
// frontend (index.html, t57.wasm, wasm_exec.js). Users can then
// open http://<addr> in a browser that supports Web Serial.
func runServe(cmd cmdServe) error {
	addr := cmd.Addr
	if addr == "" {
		addr = ":8080"
	}
	fmt.Printf("T57 web UI at http://%s\n", addr)
	fmt.Println("Open this in Chrome or Edge (Web Serial API required).")
	return http.ListenAndServe(addr, http.FileServer(webRoot()))
}

func handleSigint() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Fprintln(os.Stderr, "interrupted")
		os.Exit(130)
	}()
}
