package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "spare-test")
	if err != nil {
		panic(err)
	}
	binPath = filepath.Join(dir, "spare")
	out, err := exec.Command("go", "build", "-o", binPath, ".").CombinedOutput()
	if err != nil {
		panic("build failed: " + string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// fakeGitHub serves both the raw side (/<repo>/<ref>/cmd/<tool>/info.txt)
// and the API side (/repos/<repo>/contents/cmd) from one test server.
func fakeGitHub(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/ThiagoAVicente/spare-tools/contents/cmd", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("ref"); got != "main" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`[
			{"name": "alone", "type": "dir"},
			{"name": "waitfor", "type": "dir"},
			{"name": "README.md", "type": "file"}
		]`))
	})
	mux.HandleFunc("/ThiagoAVicente/spare-tools/main/cmd/alone/info.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("alone — single instance runner\n\nlong text here\n"))
	})
	mux.HandleFunc("/ThiagoAVicente/spare-tools/main/cmd/waitfor/info.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("waitfor — waits for conditions\n\nmore text\n"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func runSpare(t *testing.T, srv *httptest.Server, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Env = append(os.Environ(),
		"SPARE_RAW_BASE="+srv.URL,
		"SPARE_API_BASE="+srv.URL,
	)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running spare: %v", err)
	}
	return out.String(), errb.String(), code
}

func TestInfoSingleTool(t *testing.T) {
	srv := fakeGitHub(t)
	out, _, code := runSpare(t, srv, "alone")
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if !strings.Contains(out, "single instance runner") || !strings.Contains(out, "long text here") {
		t.Errorf("full info.txt not printed, got: %q", out)
	}
}

func TestUnknownTool(t *testing.T) {
	srv := fakeGitHub(t)
	out, errOut, code := runSpare(t, srv, "nosuch")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errOut, "unknown tool") {
		t.Errorf("stderr should say unknown tool, got: %q", errOut)
	}
	if out != "" {
		t.Errorf("stdout should be empty, got: %q", out)
	}
}

func TestListTools(t *testing.T) {
	srv := fakeGitHub(t)
	out, _, code := runSpare(t, srv)
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 tools (README.md filtered), got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "alone — single instance runner") {
		t.Errorf("first line should be alone's summary, got: %q", lines[0])
	}
	if !strings.Contains(lines[1], "waitfor — waits for conditions") {
		t.Errorf("second line should be waitfor's summary, got: %q", lines[1])
	}
}

func TestNetworkError(t *testing.T) {
	srv := fakeGitHub(t)
	srv.Close() // dead server → network error
	_, errOut, code := runSpare(t, srv, "alone")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if errOut == "" {
		t.Error("expected error message on stderr")
	}
}

func TestHelp(t *testing.T) {
	srv := fakeGitHub(t)
	_, errOut, code := runSpare(t, srv, "--help")
	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if !strings.Contains(errOut, "usage: spare") {
		t.Errorf("help text missing, got: %q", errOut)
	}
}

func TestURLBuilding(t *testing.T) {
	c := &client{repo: "u/r", ref: "v1", rawBase: "https://raw.example", apiBase: "https://api.example"}
	if got, want := c.infoURL("alone"), "https://raw.example/u/r/v1/cmd/alone/info.txt"; got != want {
		t.Errorf("infoURL = %q, want %q", got, want)
	}
	if got, want := c.listURL(), "https://api.example/repos/u/r/contents/cmd?ref=v1"; got != want {
		t.Errorf("listURL = %q, want %q", got, want)
	}
}

// TestRealGitHub hits the live repo; skipped unless explicitly requested.
func TestRealGitHub(t *testing.T) {
	if os.Getenv("SPARE_TEST_NETWORK") == "" {
		t.Skip("set SPARE_TEST_NETWORK=1 to test against live GitHub")
	}
	cmd := exec.Command(binPath, "alone")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("spare alone against live GitHub: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "alone") {
		t.Errorf("unexpected output: %q", out)
	}
}
