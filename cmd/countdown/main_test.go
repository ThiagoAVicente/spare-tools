package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

var binPath string

// TestMain builds the countdown binary once and runs it with pipes attached,
// so it auto-selects quiet mode (stdout is not a TTY).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "countdown-test-")
	if err != nil {
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	binPath = filepath.Join(dir, "countdown")
	out, err := exec.Command("go", "build", "-o", binPath, ".").CombinedOutput()
	if err != nil {
		os.Stderr.Write(out)
		os.RemoveAll(dir)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

func TestOneSecondPiped(t *testing.T) {
	cmd := exec.Command(binPath, "1s")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	if code := exitCode(err); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("finished too fast: %v", elapsed)
	}
	if elapsed >= 3*time.Second {
		t.Errorf("took too long: %v", elapsed)
	}
	if stdout.Len() != 0 {
		t.Errorf("expected no UI output on stdout in quiet mode, got %q", stdout.String())
	}
}

func TestInvalidDuration(t *testing.T) {
	cmd := exec.Command(binPath, "abc")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()

	if code := exitCode(err); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "invalid duration") {
		t.Errorf("stderr = %q, want it to contain %q", stderr.String(), "invalid duration")
	}
}

func TestMissingArg(t *testing.T) {
	cmd := exec.Command(binPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()

	if code := exitCode(err); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(strings.ToLower(stderr.String()), "usage") {
		t.Errorf("stderr = %q, want usage message", stderr.String())
	}
}

func TestSIGINTExits130(t *testing.T) {
	cmd := exec.Command(binPath, "10s", "--quiet")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(300 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if code := exitCode(err); code != 130 {
			t.Fatalf("exit code = %d, want 130", code)
		}
	case <-time.After(3 * time.Second):
		cmd.Process.Kill()
		t.Fatal("process did not exit promptly after SIGINT")
	}
}

func TestUpModePiped(t *testing.T) {
	cmd := exec.Command(binPath, "1s", "--up")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	if code := exitCode(err); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if elapsed < 900*time.Millisecond || elapsed >= 3*time.Second {
		t.Errorf("elapsed = %v, want ~1s", elapsed)
	}
}

func TestRenderBar(t *testing.T) {
	cases := []struct {
		frac  float64
		width int
		want  string
	}{
		{0, 10, "[░░░░░░░░░░]"},
		{0.5, 10, "[█████░░░░░]"},
		{1, 10, "[██████████]"},
		{1.5, 10, "[██████████]"},  // clamped above
		{-0.5, 10, "[░░░░░░░░░░]"}, // clamped below
		{0.25, 4, "[█░░░]"},
	}
	for _, c := range cases {
		if got := renderBar(c.frac, c.width); got != c.want {
			t.Errorf("renderBar(%v, %d) = %q, want %q", c.frac, c.width, got, c.want)
		}
	}
}

func TestFormatClock(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "00:00"},
		{5 * time.Second, "00:05"},
		{90 * time.Second, "01:30"},
		{25 * time.Minute, "25:00"},
		{time.Hour, "1:00:00"},
		{time.Hour + 30*time.Minute + 5*time.Second, "1:30:05"},
		{10*time.Hour + 2*time.Minute + 3*time.Second, "10:02:03"},
	}
	for _, c := range cases {
		if got := formatClock(c.d); got != c.want {
			t.Errorf("formatClock(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
