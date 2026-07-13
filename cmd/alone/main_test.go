package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

var binPath string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "alone-bin")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binPath = filepath.Join(tmp, "alone")
	out, err := exec.Command("go", "build", "-o", binPath, ".").CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s", err, out)
		os.RemoveAll(tmp)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

// aloneCmd builds an exec.Cmd for the built binary with XDG_RUNTIME_DIR
// pointed at a per-test temp dir.
func aloneCmd(t *testing.T, runtimeDir string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Env = append(os.Environ(), "XDG_RUNTIME_DIR="+runtimeDir)
	return cmd
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if ok := errAs(err, &ee); ok {
		return ee.ExitCode()
	}
	return -1
}

func errAs(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}

// waitLocked polls until the named lock shows up as live in --list.
func waitLocked(t *testing.T, runtimeDir, name string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		out, _ := aloneCmd(t, runtimeDir, "--list").Output()
		if strings.Contains(string(out), name) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("lock %q never became live", name)
}

func TestBusyLockExits75(t *testing.T) {
	rt := t.TempDir()
	a := aloneCmd(t, rt, "--name", "t", "sh", "-c", "sleep 5")
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		a.Process.Kill()
		a.Wait()
	}()
	waitLocked(t, rt, "t")

	marker := filepath.Join(t.TempDir(), "marker")
	start := time.Now()
	b := aloneCmd(t, rt, "--name", "t", "sh", "-c", "touch "+marker)
	err := b.Run()
	if got := exitCode(err); got != 75 {
		t.Errorf("expected exit 75, got %d (err=%v)", got, err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("B took too long: %v", elapsed)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("marker was created; command ran despite busy lock")
	}
}

func TestKilledHolderReleasesLock(t *testing.T) {
	rt := t.TempDir()
	a := aloneCmd(t, rt, "--name", "k", "sh", "-c", "sleep 60")
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	waitLocked(t, rt, "k")

	// SIGKILL the alone process itself; kernel must drop the flock.
	if err := a.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	a.Wait()

	marker := filepath.Join(t.TempDir(), "marker")
	b := aloneCmd(t, rt, "--name", "k", "sh", "-c", "touch "+marker)
	if err := b.Run(); err != nil {
		t.Fatalf("B should acquire immediately after SIGKILL of A: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("marker not created: %v", err)
	}
}

func TestWaitBlocksUntilFree(t *testing.T) {
	rt := t.TempDir()
	a := aloneCmd(t, rt, "--name", "w", "sh", "-c", "sleep 1")
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	defer a.Wait()
	waitLocked(t, rt, "w")

	marker := filepath.Join(t.TempDir(), "marker")
	start := time.Now()
	b := aloneCmd(t, rt, "--wait", "--name", "w", "sh", "-c", "touch "+marker)
	if err := b.Run(); err != nil {
		t.Fatalf("B failed: %v", err)
	}
	elapsed := time.Since(start)
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("marker not created: %v", err)
	}
	// A sleeps 1s total; B started while A held the lock, so B must have
	// waited a noticeable amount of time.
	if elapsed < 200*time.Millisecond {
		t.Errorf("B finished in %v; expected it to block on the lock", elapsed)
	}
}

func TestExitCodePropagation(t *testing.T) {
	rt := t.TempDir()
	err := aloneCmd(t, rt, "--name", "x", "--", "sh", "-c", "exit 42").Run()
	if got := exitCode(err); got != 42 {
		t.Errorf("expected exit 42, got %d", got)
	}
}

func TestSignalExitCode(t *testing.T) {
	rt := t.TempDir()
	err := aloneCmd(t, rt, "--name", "sig", "sh", "-c", "kill -TERM $$").Run()
	want := 128 + int(syscall.SIGTERM)
	if got := exitCode(err); got != want {
		t.Errorf("expected exit %d, got %d", want, got)
	}
}

func TestNameDerivation(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/usr/bin/sleep", "sleep"},
		{"sleep", "sleep"},
		{"./script.sh", "script.sh"},
		{"a/b/c", "c"},
	}
	for _, c := range cases {
		if got := deriveName(c.in); got != c.want {
			t.Errorf("deriveName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	sanCases := []struct{ in, want string }{
		{"foo/bar", "foo_bar"},
		{"plain", "plain"},
		{"a/b/c", "a_b_c"},
	}
	for _, c := range sanCases {
		if got := sanitizeName(c.in); got != c.want {
			t.Errorf("sanitizeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestList(t *testing.T) {
	rt := t.TempDir()
	a := aloneCmd(t, rt, "--name", "listy", "sh", "-c", "sleep 5")
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	waitLocked(t, rt, "listy")

	out, err := aloneCmd(t, rt, "--list").Output()
	if err != nil {
		t.Fatalf("--list failed: %v", err)
	}
	line := ""
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(l, "listy\t") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("--list did not show live lock; output: %q", out)
	}
	pid := strings.TrimPrefix(line, "listy\t")
	if pid != fmt.Sprint(a.Process.Pid) {
		t.Errorf("--list pid = %s, want %d", pid, a.Process.Pid)
	}

	a.Process.Kill()
	a.Wait()

	out, err = aloneCmd(t, rt, "--list").Output()
	if err != nil {
		t.Fatalf("--list failed: %v", err)
	}
	if strings.Contains(string(out), "listy") {
		t.Errorf("--list still shows dead lock; output: %q", out)
	}
}
