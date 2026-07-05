package t57

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// HistoryEntry is one row in the history log.
type HistoryEntry struct {
	TS   time.Time `json:"ts"`
	Port string    `json:"port,omitempty"`
	Op   string    `json:"op"`
	What string    `json:"what"`
	Data []byte    `json:"data"`
}

// History is an append-only JSONL log of recent operations.
type History struct {
	mu   sync.Mutex
	path string
	cap  int
	// In-memory cache of the most recent entries.
	cache []HistoryEntry
}

// DefaultHistoryCap caps the in-memory cache. Older entries are
// dropped first.
const DefaultHistoryCap = 256

// OpenHistory opens (or creates) the history file at `path`. If
// `path` is empty, the history is disabled.
func OpenHistory(path string) *History {
	h := &History{
		path: path,
		cap:  DefaultHistoryCap,
	}
	h.reload()
	return h
}

// DefaultHistoryPath returns the default path for the history file:
// $XDG_DATA_HOME/t57/history.jsonl, or ~/.local/share/t57/history.jsonl.
func DefaultHistoryPath() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "t57", "history.jsonl")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "t57", "history.jsonl")
}

// Path returns the path to the history file, or empty if disabled.
func (h *History) Path() string { return h.path }

// Add appends a new entry to the log. Returns an error only if the
// file cannot be opened.
func (h *History) Add(port, op, what string, data []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	entry := HistoryEntry{
		TS:   time.Now().UTC(),
		Port: port,
		Op:   op,
		What: what,
		Data: append([]byte(nil), data...),
	}
	h.cache = append(h.cache, entry)
	for len(h.cache) > h.cap {
		h.cache = h.cache[1:]
	}

	if h.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	return enc.Encode(entry)
}

// Entries returns a copy of the in-memory cache (oldest first).
func (h *History) Entries() []HistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]HistoryEntry, len(h.cache))
	copy(out, h.cache)
	return out
}

// Last returns the most recent entry, if any.
func (h *History) Last() (HistoryEntry, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.cache) == 0 {
		return HistoryEntry{}, false
	}
	return h.cache[len(h.cache)-1], true
}

// Clear truncates the history file and empties the cache.
func (h *History) Clear() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cache = nil
	if h.path == "" {
		return nil
	}
	if err := os.Remove(h.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (h *History) reload() {
	if h.path == "" {
		return
	}
	f, err := os.Open(h.path)
	if err != nil {
		return
	}
	defer f.Close()
	h.cache = nil
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e HistoryEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		h.cache = append(h.cache, e)
		if len(h.cache) > h.cap {
			h.cache = h.cache[1:]
		}
	}
}

// String returns a human-readable summary of the entry, for the CLI's
// text output.
func (e HistoryEntry) String() string {
	return fmt.Sprintf("%s  %-16s  %-12s  %-24s  %s",
		e.TS.Format("2006-01-02 15:04:05"),
		orDash(e.Port), e.Op, e.What, FormatHex(e.Data))
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
