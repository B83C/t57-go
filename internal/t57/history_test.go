package t57

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHistoryAddAndRead(t *testing.T) {
	dir := t.TempDir()
	h := OpenHistory(filepath.Join(dir, "history.jsonl"))
	h.Add("auto", "read", "block 1", []byte{1, 2, 3, 4})
	h.Add("auto", "read", "block 2", []byte{5, 6, 7, 8})

	es := h.Entries()
	if len(es) != 2 {
		t.Fatalf("entries = %d, want 2", len(es))
	}
	if es[0].Op != "read" || es[1].Op != "read" {
		t.Fatalf("ops = %v", es)
	}
	if !strings.Contains(es[0].String(), "read") {
		t.Fatalf("String() = %q", es[0].String())
	}
}

func TestHistoryReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")
	h1 := OpenHistory(path)
	h1.Add("auto", "read", "block 1", []byte{0xDE, 0xAD, 0xBE, 0xEF})
	h1.Add("auto", "read", "block 2", []byte{0xCA, 0xFE})

	// Reopen and verify the entries are still there.
	h2 := OpenHistory(path)
	es := h2.Entries()
	if len(es) != 2 {
		t.Fatalf("entries = %d, want 2", len(es))
	}
	if es[0].What != "block 1" {
		t.Fatalf("what = %q, want block 1", es[0].What)
	}
}

func TestHistoryClear(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")
	h := OpenHistory(path)
	h.Add("auto", "read", "block 1", []byte{1, 2, 3, 4})
	if err := h.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("history file still exists")
	}
	if _, ok := h.Last(); ok {
		t.Fatalf("Last returned ok after Clear")
	}
}

func TestHistoryCap(t *testing.T) {
	dir := t.TempDir()
	h := &History{
		path: filepath.Join(dir, "history.jsonl"),
		cap:  3,
	}
	for i := 0; i < 10; i++ {
		h.Add("auto", "read", "x", []byte{byte(i)})
	}
	if len(h.Entries()) != 3 {
		t.Fatalf("entries = %d, want 3", len(h.Entries()))
	}
	if e, _ := h.Last(); e.Data[0] != 9 {
		t.Fatalf("last = %d, want 9", e.Data[0])
	}
}

func TestHistoryEntryJSON(t *testing.T) {
	e := HistoryEntry{
		TS:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Port: "auto",
		Op:   "read",
		What: "block 1",
		Data: []byte{1, 2, 3, 4},
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"op":"read"`) {
		t.Fatalf("encoded = %s (missing op)", s)
	}
}
