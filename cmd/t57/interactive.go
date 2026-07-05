package main

import (
	"fmt"
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
		blks, err := m.client.ReadBlocks(1, 6)
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
		case "d", "D":
			m.blocks[0] = t57.FactoryDefault().LEBytes()
			m.changed[0] = true
			m.status = "Config set to factory default (write to apply)"
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


