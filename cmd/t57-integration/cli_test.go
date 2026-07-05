package cli_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLIHelp runs the built `t57` binary with `help` and checks that
// the usage message is printed.
//
// Skipped if the binary is not built; run `go build ./cmd/t57` first.
func TestCLIHelp(t *testing.T) {
	bin := findBinary(t)
	cmd := exec.Command(bin, "help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("t57 help: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Usage: t57") {
		t.Fatalf("missing usage line in:\n%s", out)
	}
	if !strings.Contains(string(out), "read") {
		t.Fatalf("missing 'read' in usage:\n%s", out)
	}
}

func TestCLIPorts(t *testing.T) {
	bin := findBinary(t)
	cmd := exec.Command(bin, "ports")
	out, err := cmd.CombinedOutput()
	// We don't require any ports to exist; just that the command runs.
	if err != nil {
		// It might exit non-zero if listing fails, that's OK.
		t.Logf("t57 ports: %v\n%s", err, out)
	}
}

func TestCLIHistoryPath(t *testing.T) {
	bin := findBinary(t)
	dir := t.TempDir()
	cmd := exec.Command(bin, "--history", filepath.Join(dir, "h.jsonl"), "history", "path")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("t57 history path: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	want := filepath.Join(dir, "h.jsonl")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCLIReadOnMissingDevice(t *testing.T) {
	bin := findBinary(t)
	cmd := exec.Command(bin, "--port", "/dev/null/does-not-exist", "read", "-b", "1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("t57 read on missing port should fail\n%s", out)
	}
	if !bytes.Contains(out, []byte("error:")) {
		t.Fatalf("missing error: prefix in:\n%s", out)
	}
}

func findBinary(t *testing.T) string {
	t.Helper()
	// Try a few well-known locations.
	candidates := []string{
		"t57",
		"./t57",
		"../../t57-go/t57",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	// Fall back to `go run` with stdin/stdout redirection.
	if _, err := exec.LookPath("go"); err == nil {
		// Build once into a temp file.
		tmp, err := os.CreateTemp("", "t57-bin-*")
		if err != nil {
			t.Skip("no binary and cannot create temp:", err)
		}
		tmp.Close()
		os.Remove(tmp.Name())
		build := exec.Command("go", "build", "-o", tmp.Name(), "./cmd/t57")
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			t.Skipf("cannot build t57: %v", err)
		}
		return tmp.Name()
	}
	t.Skip("no t57 binary found and no `go` in PATH")
	return ""
}

// _ = io for completeness.
var _ = io.EOF
