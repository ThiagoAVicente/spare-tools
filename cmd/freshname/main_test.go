package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "freshname-bin")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	binPath = filepath.Join(dir, "freshname")
	if runtimeIsWindows() {
		binPath += ".exe"
	}

	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		panic("failed to build freshname: " + err.Error() + "\n" + string(out))
	}

	os.Exit(m.Run())
}

func runtimeIsWindows() bool {
	return os.PathSeparator == '\\'
}

func runBin(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			t.Fatalf("failed to run binary: %v", err)
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

func touch(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("touch %s: %v", path, err)
	}
	f.Close()
}

func TestFreshNameNotExists(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.txt")

	stdout, stderr, code := runBin(t, target)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	want := target + "\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestFreshNameOneCollision(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.txt")
	touch(t, target)

	stdout, stderr, code := runBin(t, target)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	want := filepath.Join(dir, "a-2.txt") + "\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestFreshNameTwoCollisions(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.txt")
	touch(t, target)
	touch(t, filepath.Join(dir, "a-2.txt"))

	stdout, stderr, code := runBin(t, target)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	want := filepath.Join(dir, "a-3.txt") + "\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestFreshNameCompoundExt(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "b.tar.gz")
	touch(t, target)

	stdout, stderr, code := runBin(t, target)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	want := filepath.Join(dir, "b-2.tar.gz") + "\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestFreshNameDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "pasta")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runBin(t, target)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	want := filepath.Join(dir, "pasta-2") + "\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestFreshNameCreate(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.txt")
	touch(t, target)

	stdout, stderr, code := runBin(t, "--create", target)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	wantName := filepath.Join(dir, "a-2.txt")
	want := wantName + "\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if _, err := os.Stat(wantName); err != nil {
		t.Errorf("expected file %s to exist: %v", wantName, err)
	}
}

func TestFreshNameConcurrentCreate(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "race.txt")

	const n = 20
	var wg sync.WaitGroup
	names := make([]string, n)
	errs := make([]string, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			stdout, stderr, code := runBin(t, "--create", target)
			if code != 0 {
				errs[i] = stderr
				return
			}
			names[i] = stdout
		}(i)
	}
	wg.Wait()

	seen := make(map[string]bool)
	for i, e := range errs {
		if e != "" {
			t.Errorf("process %d failed: %s", i, e)
		}
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		if seen[name] {
			t.Errorf("duplicate name printed: %q", name)
		}
		seen[name] = true
	}
	if len(seen) != n {
		t.Errorf("expected %d distinct names, got %d: %v", n, len(seen), seen)
	}

	for name := range seen {
		trimmed := name[:len(name)-1] // strip trailing newline
		if _, err := os.Stat(trimmed); err != nil {
			t.Errorf("expected file %s to exist: %v", trimmed, err)
		}
	}
}

func TestFreshNameSepAndStart(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "foto.jpg")
	touch(t, target)

	stdout, stderr, code := runBin(t, "--sep", "_", "--start", "1", target)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %s", code, stderr)
	}
	want := filepath.Join(dir, "foto_1.jpg") + "\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestFreshNameMissingArg(t *testing.T) {
	stdout, stderr, code := runBin(t)
	if code != 2 {
		t.Errorf("exit code = %d, want 2 (stdout=%q stderr=%q)", code, stdout, stderr)
	}
}

func TestFreshNameHelpMentionsRace(t *testing.T) {
	cmd := exec.Command(binPath, "--help")
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	_ = cmd.Run()
	combined := outBuf.String() + errBuf.String()
	if !bytes.Contains([]byte(combined), []byte("--create")) {
		t.Errorf("--help output should recommend --create for concurrent scripts, got: %s", combined)
	}
}
