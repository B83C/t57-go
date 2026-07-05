package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/B83C/t57-go/internal/serial"
	"github.com/B83C/t57-go/internal/t57"
)

type model struct {
	client *t57.Client
	port   *serial.Transport

	blocks  [8][4]byte
	changed [8]bool
	curB    int // cursor block 0..7
	curX    int // cursor byte 0..3
	pending bool // waiting for 2nd hex digit

	status string
	sn     string
	fw     string
	err    error

	connected bool
	quitting  bool

	confirmWrite bool // waiting for y/n confirm before writing
}

type connectMsg struct {
	client *t57.Client
	port   *serial.Transport
	sn     string
	fw     string
	err    error
}
type readDoneMsg struct {
	blocks [8][4]byte
	err    error
}
type writeDoneMsg struct {
	n   int
	err error
}

func initialModel(c *args) model {
	return model{
		status: "Connecting…",
	}
}

var globalArgs *args

func runTUI(c *args) error {
	globalArgs = c
	p := tea.NewProgram(initialModel(c))
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

func connectCmd(a *args) tea.Cmd {
	return func() tea.Msg {
		cli, err := openClient(a)
		if err != nil {
			return connectMsg{err: err}
		}
		sn, _ := cli.SerialNumber()
		ver, _ := cli.FirmwareVersion()
		return connectMsg{
			client: cli,
			sn:     fmt.Sprintf("%X", sn),
			fw:     strings.TrimRight(string(ver), "\x00 "),
		}
	}
}

func readAllCmd(m *model) tea.Cmd {
	return func() tea.Msg {
		cfg, err := m.client.ReadConfig()
		if err != nil {
			return readDoneMsg{err: err}
		}
		blks, err := m.client.ReadBlocks(1, 7)
		if err != nil {
			return readDoneMsg{err: err}
		}
		var out [8][4]byte
		out[0] = cfg.LEBytes()
		for i, b := range blks {
			out[i+1] = b
		}
		return readDoneMsg{blocks: out}
	}
}

func writeAllCmd(m *model) tea.Cmd {
	return func() tea.Msg {
		// Write all 8 blocks (config + user) to the device.
		for bi := 0; bi < 8; bi++ {
			if bi == 0 {
				if err := m.client.WriteConfig(t57.ConfigFromLEBytes(m.blocks[0])); err != nil {
					return writeDoneMsg{err: fmt.Errorf("config: %w", err)}
				}
			} else {
				if err := m.client.WriteBlock(uint8(bi), m.blocks[bi]); err != nil {
					return writeDoneMsg{err: fmt.Errorf("block %d: %w", bi, err)}
				}
			}
		}
		for i := range m.changed {
			m.changed[i] = false
		}
		return writeDoneMsg{n: 8}
	}
}

func writeChangedCmd(m *model) tea.Cmd {
	return func() tea.Msg {
		// Snapshot changed indices so the goroutine doesn't race.
		var toWrite []struct{ idx int; data [4]byte }
		for bi := 0; bi < 8; bi++ {
			if m.changed[bi] {
				toWrite = append(toWrite, struct {
					idx  int
					data [4]byte
				}{idx: bi, data: m.blocks[bi]})
			}
		}
		for _, w := range toWrite {
			if w.idx == 0 {
				if err := m.client.WriteConfig(t57.ConfigFromLEBytes(w.data)); err != nil {
					return writeDoneMsg{err: err}
				}
			} else {
				if err := m.client.WriteBlock(uint8(w.idx), w.data); err != nil {
					return writeDoneMsg{err: err}
				}
			}
		}
		return writeDoneMsg{n: len(toWrite)}
	}
}

func (m model) Init() tea.Cmd {
	// Connect but don't read anything.  User presses 'r' to read.
	if globalArgs != nil {
		return connectCmd(globalArgs)
	}
	return connectCmd(&args{Baud: 9600, Retries: 2})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case connectMsg:
		if msg.err != nil {
			m.err = msg.err
			m.status = "Connect: " + msg.err.Error()
			return m, tea.Quit
		}
		m.client = msg.client
		m.sn = msg.sn
		m.fw = msg.fw
		m.connected = true
		m.status = "Connected — press R to read blocks"

	case readDoneMsg:
		if msg.err != nil {
			m.status = "Read error: " + msg.err.Error()
		} else {
			m.blocks = msg.blocks
			for i := range m.changed {
				m.changed[i] = false
			}
			m.pending = false
			m.status = "Read 8 blocks"
		}
		return m, nil

	case writeDoneMsg:
		if msg.err != nil {
			m.status = "Write error: " + msg.err.Error()
		} else {
			for i := range m.changed {
				m.changed[i] = false
			}
			m.pending = false
			m.status = fmt.Sprintf("Wrote %d block(s)", msg.n)
		}
		return m, nil

	case tea.KeyMsg:
		if !m.connected {
			return m, nil
		}
		s := msg.String()
		switch s {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "r":
			m.status = "Reading…"
			return m, readAllCmd(&m)
		case "w":
			// Count changed blocks
			n := 0
			for _, ch := range m.changed {
				if ch {
					n++
				}
			}
			if n == 0 {
				m.status = "Nothing changed"
				return m, nil
			}
			m.confirmWrite = true
			m.status = fmt.Sprintf("Write %d changed block(s)? [y/N] ", n)
			return m, nil
		case "u":
			return m, readAllCmd(&m)
		case "d":
			m.blocks[0] = t57.FactoryDefault().LEBytes()
			m.changed[0] = true
			m.status = "Config set to factory default (press w to write)"
			return m, nil
		case "D":
			if !m.connected {
				m.status = "Not connected"
				return m, nil
			}
			m.status = "Writing all 8 blocks…"
			return m, writeAllCmd(&m)
		case "s":
			if err := saveSnapshot(m.blocks); err != nil {
				m.status = "Save: " + err.Error()
			} else {
				m.status = "Snapshot saved"
			}
			return m, nil
		case "S":
			blocks, err := loadSnapshot()
			if err != nil {
				m.status = "Load: " + err.Error()
			} else {
				m.blocks = blocks
				for i := range m.changed {
					m.changed[i] = false
				}
				m.pending = false
				m.status = "Snapshot loaded"
			}
			return m, nil
		case "up", "k":
			if m.curB > 0 {
				m.curB--
				m.pending = false
			}
		case "down", "j":
			if m.curB < 7 {
				m.curB++
				m.pending = false
			}
		case "left", "h":
			if m.curX > 0 {
				m.curX--
				m.pending = false
			}
		case "right", "l":
			if m.curX < 3 {
				m.curX++
				m.pending = false
			}
		case "tab":
			m.curX = (m.curX + 1) % 4
			m.pending = false

		case "y", "Y":
			if m.confirmWrite {
				m.confirmWrite = false
				m.status = "Writing…"
				return m, writeChangedCmd(&m)
			}
		case "n", "N", "esc":
			if m.confirmWrite {
				m.confirmWrite = false
				m.status = "Write cancelled"
				return m, nil
			}

		default:
			if m.confirmWrite {
				m.confirmWrite = false
				m.status = "Write cancelled"
				return m, nil
			}
			if len(s) == 1 {
				b := s[0]
				if h := hexVal(b); h >= 0 {
					if m.pending {
						// Second hex digit → low nibble
						m.blocks[m.curB][m.curX] = (m.blocks[m.curB][m.curX] & 0xF0) | byte(h)
						m.changed[m.curB] = true
						m.pending = false
						// Advance cursor
						if m.curX < 3 {
							m.curX++
						} else if m.curB < 7 {
							m.curB++
							m.curX = 0
						}
					} else {
						// First hex digit → high nibble
						m.blocks[m.curB][m.curX] = (byte(h) << 4) | (m.blocks[m.curB][m.curX] & 0x0F)
						m.changed[m.curB] = true
						m.pending = true
					}
				}
			}
		}
	}
	return m, nil
}

func (m model) View() string {
	if !m.connected && m.err != nil && m.status == "Connecting…" {
		return "Connecting…\n"
	}
	if !m.connected && m.err != nil {
		return fmt.Sprintf("Connect failed: %v\n", m.err)
	}
	var b strings.Builder
	b.WriteString("\n  T57 RFID — Hex Editor\n")
	b.WriteString(fmt.Sprintf("  SN: %s  FW: %s\n\n", m.sn, m.fw))

	for bi := 0; bi < 8; bi++ {
		label := fmt.Sprintf("Block %d", bi)
		if bi == 0 {
			label = "Config"
		}
		mark := ' '
		if m.changed[bi] {
			mark = '*'
		}
		// Build the hex bytes
		hexStr := ""
		for bj := 0; bj < 4; bj++ {
			val := m.blocks[bi][bj]
			sel := (bi == m.curB && bj == m.curX)
			if sel {
				if m.pending {
					hexStr += fmt.Sprintf("\x1b[43;30m[%02X]\x1b[0m ", val)
				} else {
					hexStr += fmt.Sprintf("\x1b[7m[%02X]\x1b[0m ", val)
				}
			} else if m.changed[bi] {
				hexStr += fmt.Sprintf("\x1b[33m[%02X]\x1b[0m ", val)
			} else {
				hexStr += fmt.Sprintf("[%02X] ", val)
			}
		}
		b.WriteString(fmt.Sprintf("  %-12s %c %s\n", label, mark, hexStr))
	}

	b.WriteString("\n")
	b.WriteString("  [R]ead  [W]rite  [D]efault config  [U]ndo  [Q]uit\n")
	b.WriteString(fmt.Sprintf("  %s\n", m.status))
	return b.String()
}

func saveSnapshot(blocks [8][4]byte) error {
	path, err := snapshotPath()
	if err != nil {
		return err
	}
	data := make([]byte, 32)
	for bi := 0; bi < 8; bi++ {
		copy(data[bi*4:], blocks[bi][:])
	}
	return os.WriteFile(path, data, 0644)
}

func loadSnapshot() ([8][4]byte, error) {
	var out [8][4]byte
	path, err := snapshotPath()
	if err != nil {
		return out, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	if len(data) != 32 {
		return out, fmt.Errorf("snapshot file: expected 32 bytes, got %d", len(data))
	}
	for bi := 0; bi < 8; bi++ {
		copy(out[bi][:], data[bi*4:bi*4+4])
	}
	return out, nil
}

func snapshotPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".local", "share", "t57")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "snapshot.bin"), nil
}

func hexVal(b byte) int {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0')
	case b >= 'a' && b <= 'f':
		return int(b - 'a' + 10)
	case b >= 'A' && b <= 'F':
		return int(b - 'A' + 10)
	}
	return -1
}


