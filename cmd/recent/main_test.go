package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "recent-bin")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binPath = filepath.Join(dir, "recent")
	out, err := exec.Command("go", "build", "-o", binPath, ".").CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// run executes the built binary with stdout piped (non-TTY -> raw format).
// XDG_CONFIG_HOME points at an empty dir so the user's real config never
// leaks into tests.
func run(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+t.TempDir())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("recent %v: %v\nstderr: %s", args, err, stderr.String())
	}
	return stdout.String()
}

// paths extracts the path column from raw output lines (<mtime>\t<bytes>\t<path>).
func paths(t *testing.T, out string) []string {
	t.Helper()
	var ps []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			t.Fatalf("raw line has %d fields, want 3: %q", len(fields), line)
		}
		ps = append(ps, fields[2])
	}
	return ps
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func TestWindowFilter(t *testing.T) {
	dir := t.TempDir()
	newFile := filepath.Join(dir, "new.txt")
	oldFile := filepath.Join(dir, "old.txt")
	writeFile(t, newFile)
	writeFile(t, oldFile)
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(oldFile, old, old); err != nil {
		t.Fatal(err)
	}

	got := paths(t, run(t, "10m", dir))
	if !contains(got, newFile) {
		t.Errorf("10m window: missing %s in %v", newFile, got)
	}
	if contains(got, oldFile) {
		t.Errorf("10m window: stale file %s present in %v", oldFile, got)
	}

	got = paths(t, run(t, "3h", dir))
	if !contains(got, newFile) || !contains(got, oldFile) {
		t.Errorf("3h window: want both files, got %v", got)
	}
}

func TestDefaultExclusions(t *testing.T) {
	dir := t.TempDir()
	excluded := filepath.Join(dir, "node_modules", "lixo.js")
	kept := filepath.Join(dir, "ok.txt")
	writeFile(t, excluded)
	writeFile(t, kept)

	got := paths(t, run(t, "10m", dir))
	if contains(got, excluded) {
		t.Errorf("default run: node_modules file should be excluded, got %v", got)
	}
	if !contains(got, kept) {
		t.Errorf("default run: missing %s in %v", kept, got)
	}

	got = paths(t, run(t, "10m", dir, "--all"))
	if !contains(got, excluded) {
		t.Errorf("--all: node_modules file should be present, got %v", got)
	}
}

func TestNewestFirst(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	want := []string{
		filepath.Join(dir, "c.txt"), // newest
		filepath.Join(dir, "b.txt"),
		filepath.Join(dir, "a.txt"), // oldest
	}
	for i, p := range want {
		writeFile(t, p)
		ts := now.Add(-time.Duration(i) * time.Minute)
		if err := os.Chtimes(p, ts, ts); err != nil {
			t.Fatal(err)
		}
	}

	got := paths(t, run(t, "10m", dir))
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order mismatch: want %v, got %v", want, got)
		}
	}
}

func TestNulSeparated(t *testing.T) {
	dir := t.TempDir()
	tricky := filepath.Join(dir, "we ird's file.txt")
	writeFile(t, tricky)

	out := run(t, "-0", "10m", dir)
	parts := strings.Split(out, "\x00")
	if parts[len(parts)-1] != "" {
		t.Fatalf("output not NUL-terminated: %q", out)
	}
	parts = parts[:len(parts)-1]
	if !contains(parts, tricky) {
		t.Errorf("-0: path with spaces/quotes mangled: %q", parts)
	}
	for _, p := range parts {
		if strings.Contains(p, "\t") || strings.Contains(p, "\n") {
			t.Errorf("-0: unexpected extra fields in %q", p)
		}
	}
}

func TestDirsFlag(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got := paths(t, run(t, "10m", dir))
	if contains(got, sub) {
		t.Errorf("default run should not list directories, got %v", got)
	}
	got = paths(t, run(t, "10m", dir, "--dirs"))
	if !contains(got, sub) {
		t.Errorf("--dirs should list %s, got %v", sub, got)
	}
}

func TestTopLimit(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		writeFile(t, filepath.Join(dir, n))
	}
	got := paths(t, run(t, "10m", dir, "--top", "2"))
	if len(got) != 2 {
		t.Errorf("--top 2: want 2 lines, got %d: %v", len(got), got)
	}
}

// --- Unit tests ---

func TestHumanSize(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0"},
		{500, "500"},
		{1024, "1.0K"},
		{1500000, "1.4M"},
		{10 * 1024 * 1024, "10M"},
		{2 * 1024 * 1024 * 1024, "2.0G"},
	}
	for _, c := range cases {
		if got := humanSize(c.in); got != c.want {
			t.Errorf("humanSize(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRelTime(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "30s ago"},
		{2 * time.Minute, "2m ago"},
		{3 * time.Hour, "3h ago"},
		{26 * time.Hour, "1d ago"},
	}
	for _, c := range cases {
		if got := relTime(c.in); got != c.want {
			t.Errorf("relTime(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExclusionMatcher(t *testing.T) {
	excl := defaultExcludes
	dirCases := []struct {
		path string
		want bool
	}{
		{"/home/u/proj/node_modules", true},
		{"/home/u/.git", true},
		{"/home/u/.cache", true},
		{"/home/u/proj/target", true},
		{"/home/u/.venv", true},
		{"/home/u/proj/__pycache__", true},
		{"/home/u/.local/share/Trash", true},
		{"/home/u/share/Trash", false},
		{"/home/u/src", false},
		{"/home/u/gitrepo", false},
	}
	for _, c := range dirCases {
		if got := isExcludedDir(c.path, filepath.Base(c.path), excl); got != c.want {
			t.Errorf("isExcludedDir(%q) = %v, want %v", c.path, got, c.want)
		}
	}

	fileCases := []struct {
		name string
		want bool
	}{
		{".main.go.swp", true},
		{"notes.txt~", true},
		{"main.go", false},
		{"swp", false},
	}
	for _, c := range fileCases {
		if got := isExcludedFile(c.name); got != c.want {
			t.Errorf("isExcludedFile(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestParseExcludeLine(t *testing.T) {
	got, ok := parseExcludeLine(`exclude = ["a", "b"]`)
	if !ok || len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf(`parseExcludeLine: got %v ok=%v, want [a b] true`, got, ok)
	}
	if got, ok := parseExcludeLine(`exclude = []`); !ok || len(got) != 0 {
		t.Errorf("empty array: got %v ok=%v", got, ok)
	}
	if _, ok := parseExcludeLine(`exclude = [a, b]`); ok {
		t.Error("unquoted items should fail")
	}
	if _, ok := parseExcludeLine(`exclude = "a"`); ok {
		t.Error("non-array value should fail")
	}
	if _, ok := parseExcludeLine(`other = ["a"]`); ok {
		t.Error("wrong key should not match")
	}
}
