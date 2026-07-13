package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "waitfor-bin")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mkdtemp:", err)
		os.Exit(1)
	}
	binPath = filepath.Join(dir, "waitfor")
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

// run executes the built binary with args and returns exit code, stderr, and elapsed time.
func run(t *testing.T, args ...string) (int, string, time.Duration) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	var stderr, stdout []byte
	errPipe, _ := cmd.StderrPipe()
	outPipe, _ := cmd.StdoutPipe()
	start := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	buf := make([]byte, 4096)
	for {
		n, err := errPipe.Read(buf)
		stderr = append(stderr, buf[:n]...)
		if err != nil {
			break
		}
	}
	for {
		n, err := outPipe.Read(buf)
		stdout = append(stdout, buf[:n]...)
		if err != nil {
			break
		}
	}
	err := cmd.Wait()
	elapsed := time.Since(start)
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("wait: %v", err)
		}
	}
	return code, string(stderr), elapsed
}

func TestPidExit(t *testing.T) {
	t.Parallel()
	sleep := exec.Command("sleep", "1")
	if err := sleep.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	pid := sleep.Process.Pid
	go sleep.Wait()

	code, stderr, elapsed := run(t, "--pid", fmt.Sprint(pid), "--timeout", "10s")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr)
	}
	if elapsed < 800*time.Millisecond {
		t.Fatalf("returned too early: %v", elapsed)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("returned too late: %v", elapsed)
	}
}

func TestPidAlreadyDead(t *testing.T) {
	t.Parallel()
	proc := exec.Command("true")
	if err := proc.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := proc.Process.Pid
	proc.Wait()

	code, stderr, elapsed := run(t, "--pid", fmt.Sprint(pid), "--timeout", "5s")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("dead pid should return immediately, took %v", elapsed)
	}
}

func TestPortClosedTimeout(t *testing.T) {
	t.Parallel()
	// Reserve a port, then close so nothing is listening.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	code, _, elapsed := run(t, "--port", fmt.Sprint(port), "--timeout", "2s")
	if code != 124 {
		t.Fatalf("exit code = %d, want 124", code)
	}
	if elapsed < 1800*time.Millisecond || elapsed > 5*time.Second {
		t.Fatalf("elapsed = %v, want ~2s", elapsed)
	}
}

func TestPortOpen(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	code, stderr, elapsed := run(t, "--port", fmt.Sprint(port), "--timeout", "5s")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("open port should be fast, took %v", elapsed)
	}
}

func TestPortDelayedListener(t *testing.T) {
	t.Parallel()
	// Find a free port first.
	ln0, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln0.Addr().(*net.TCPAddr).Port
	ln0.Close()

	done := make(chan net.Listener, 1)
	go func() {
		time.Sleep(500 * time.Millisecond)
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			done <- ln
		}
	}()
	defer func() {
		select {
		case ln := <-done:
			ln.Close()
		case <-time.After(3 * time.Second):
		}
	}()

	code, stderr, elapsed := run(t, "--port", fmt.Sprintf("127.0.0.1:%d", port), "--timeout", "10s")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr)
	}
	if elapsed < 400*time.Millisecond {
		t.Fatalf("returned before listener opened: %v", elapsed)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("took too long: %v", elapsed)
	}
}

func TestExistsDelayed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "target")
	go func() {
		time.Sleep(500 * time.Millisecond)
		os.WriteFile(path, []byte("x"), 0o644)
	}()

	code, stderr, elapsed := run(t, "--exists", path, "--timeout", "10s")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr)
	}
	if elapsed < 400*time.Millisecond {
		t.Fatalf("returned before file was created: %v", elapsed)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("took too long: %v", elapsed)
	}
}

func TestExistsAlready(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "pre")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stderr, elapsed := run(t, "--exists", path, "--timeout", "5s")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("pre-existing file should be immediate, took %v", elapsed)
	}
}

func TestGone(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "doomed")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Remove(path)
	}()

	code, stderr, elapsed := run(t, "--gone", path, "--timeout", "10s")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr)
	}
	if elapsed < 400*time.Millisecond {
		t.Fatalf("returned before file was removed: %v", elapsed)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("took too long: %v", elapsed)
	}
}

func TestStable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "growing")
	if err := os.WriteFile(path, []byte("start"), 0o644); err != nil {
		t.Fatal(err)
	}
	go func() {
		// Append every 200ms for ~1s total (5 writes).
		for i := 0; i < 5; i++ {
			time.Sleep(200 * time.Millisecond)
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
			if err == nil {
				f.WriteString("more\n")
				f.Close()
			}
		}
	}()

	code, stderr, elapsed := run(t, "--file", path, "--stable", "0.8s", "--timeout", "15s")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr)
	}
	// Last write ~1s in; stable 800ms after that => >= ~1.6s total.
	if elapsed < 1600*time.Millisecond {
		t.Fatalf("returned before file was stable: %v", elapsed)
	}
	if elapsed > 8*time.Second {
		t.Fatalf("took too long: %v", elapsed)
	}
}

func TestExistsMissingParent(t *testing.T) {
	t.Parallel()
	code, stderr, _ := run(t, "--exists", "/nonexistent-dir-waitfor-test/sub/file")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr: %s", code, stderr)
	}
	if stderr == "" {
		t.Fatal("expected error message on stderr")
	}
}

func TestNoConditions(t *testing.T) {
	t.Parallel()
	code, stderr, _ := run(t, "--timeout", "1s")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr: %s", code, stderr)
	}
	if stderr == "" {
		t.Fatal("expected usage message on stderr")
	}
}

func TestStableWithoutFile(t *testing.T) {
	t.Parallel()
	code, _, _ := run(t, "--stable", "1s")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}
