package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "notify-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	binPath = filepath.Join(dir, "notify")
	build := exec.Command("go", "build", "-o", binPath, ".")
	out, err := build.CombinedOutput()
	if err != nil {
		panic("build failed: " + err.Error() + "\n" + string(out))
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func runBin(t *testing.T, args ...string) (exitCode int, stderr string) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err == nil {
		return 0, errBuf.String()
	}
	var ee *exec.ExitError
	if ok := isExitError(err, &ee); !ok {
		t.Fatalf("running %v: %v", args, err)
	}
	return ee.ExitCode(), errBuf.String()
}

func isExitError(err error, out **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*out = ee
	}
	return ok
}

func TestExitCodePropagated(t *testing.T) {
	code, _ := runBin(t, "--", "sh", "-c", "exit 7")
	if code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
}

func TestExitCodeZero(t *testing.T) {
	code, _ := runBin(t, "sh", "-c", "exit 0")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestMissingCommand(t *testing.T) {
	code, stderr := runBin(t, "this-command-definitely-does-not-exist-xyz")
	if code != 127 {
		t.Errorf("exit code = %d, want 127", code)
	}
	if stderr == "" {
		t.Errorf("expected error message on stderr")
	}
}

func TestNoCommandGiven(t *testing.T) {
	code, stderr := runBin(t)
	if code == 0 {
		t.Errorf("exit code = 0, want non-zero when no command given")
	}
	if stderr == "" {
		t.Errorf("expected usage message on stderr")
	}
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantTitle string
		wantSound bool
		wantCmd   []string
	}{
		{
			name:      "double dash separates tool flags from child flags",
			args:      []string{"--sound", "--", "cmd", "--sound"},
			wantSound: true,
			wantCmd:   []string{"cmd", "--sound"},
		},
		{
			name:    "flags after command go to child",
			args:    []string{"ls", "--color"},
			wantCmd: []string{"ls", "--color"},
		},
		{
			name:      "title flag with value",
			args:      []string{"--title", "X", "cmd"},
			wantTitle: "X",
			wantCmd:   []string{"cmd"},
		},
		{
			name:      "title equals form",
			args:      []string{"--title=My Build", "make"},
			wantTitle: "My Build",
			wantCmd:   []string{"make"},
		},
		{
			name:      "sound then command",
			args:      []string{"--sound", "sleep", "1"},
			wantSound: true,
			wantCmd:   []string{"sleep", "1"},
		},
		{
			name:    "bare double dash",
			args:    []string{"--", "ls"},
			wantCmd: []string{"ls"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, cmd, err := parseArgs(tt.args)
			if err != nil {
				t.Fatalf("parseArgs(%v) error: %v", tt.args, err)
			}
			if opts.title != tt.wantTitle {
				t.Errorf("title = %q, want %q", opts.title, tt.wantTitle)
			}
			if opts.sound != tt.wantSound {
				t.Errorf("sound = %v, want %v", opts.sound, tt.wantSound)
			}
			if len(cmd) != len(tt.wantCmd) {
				t.Fatalf("cmd = %v, want %v", cmd, tt.wantCmd)
			}
			for i := range cmd {
				if cmd[i] != tt.wantCmd[i] {
					t.Errorf("cmd = %v, want %v", cmd, tt.wantCmd)
					break
				}
			}
		})
	}
}

func TestParseArgsErrors(t *testing.T) {
	if _, _, err := parseArgs([]string{"--title"}); err == nil {
		t.Errorf("parseArgs(--title with no value) should error")
	}
}

func TestHelp(t *testing.T) {
	cmd := exec.Command(binPath, "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--help exited non-zero: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "notify") || !strings.Contains(string(out), "--sound") {
		t.Errorf("help output missing expected content:\n%s", out)
	}
}

func TestRealNotification(t *testing.T) {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Skip("no D-Bus session bus available")
	}
	code, stderr := runBin(t, "--title", "notify test", "true")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if strings.Contains(stderr, "notify:") {
		t.Errorf("unexpected warning with live bus: %s", stderr)
	}
}

func TestSignalKilledChild(t *testing.T) {
	code, _ := runBin(t, "sh", "-c", "kill -TERM $$")
	if code != 143 {
		t.Errorf("exit code = %d, want 143 (128+SIGTERM)", code)
	}
}
