# spare-tools v0.1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Six independent Go CLI tools (alone, notify, waitfor, countdown, freshname, recent) sharing one module, one exit-code convention, and one duration parser, plus an `install.sh <tool>` installer.

**Architecture:** Single Go module `github.com/ThiagoAVicente/spare-tools`. Each tool is a standalone binary under `cmd/<tool>/`. Shared code (duration parser, exit codes, TTY detection) lives in `internal/comum`. Tools are uncorrelated — each `cmd/<tool>` task can be built in parallel by a separate agent, touching only its own directory.

**Tech Stack:** Go 1.26, `golang.org/x/sys/unix` (flock, pidfd_open, statx), `golang.org/x/term` (raw mode, TTY detect), `github.com/fsnotify/fsnotify` (inotify), `github.com/godbus/dbus/v5` (desktop notifications). Std `flag` package for CLI parsing. All deps already in `go.mod` — tasks must NOT modify `go.mod`/`go.sum`.

**Conventions (all tools):**
- Exit codes from `internal/comum`: 0 OK, 75 lock busy, 124 timeout, 130 canceled.
- `--help` with usage + examples (custom `flag.Usage`).
- Errors to stderr, machine-usable output to stdout.
- Tests: unit tests for pure logic; integration tests build the binary via `go test` helper (`os/exec` on `go run ./cmd/<tool>` or a TestMain-built binary). Tests needing D-Bus/TTY/desktop: `t.Skip` when environment lacks them.
- `gofmt` clean, `go vet` clean.

---

### Task 0: Skeleton (DONE — pre-built in main session)

- [x] `go.mod` with deps, `internal/comum` (ParseDuration, FormatDuration, exit codes, StdoutIsTTY) + passing tests
- [x] `install.sh` (build one/all to `$BINDIR`, default `~/.local/bin`, overwrite = update)
- [x] README, CI workflow, .gitignore

---

### Task 1: `alone` — single-instance command runner

**Files:** Create `cmd/alone/main.go`, `cmd/alone/main_test.go`

**CLI contract:**
```
alone [--wait] [--name NAME] [--list] [--] CMD [ARGS...]
```
- Lock file: `$XDG_RUNTIME_DIR/alone/<name>.lock` (fallback `/tmp/alone-$UID/` if XDG_RUNTIME_DIR unset), dir created 0700.
- Name: `--name` or derived from command basename, sanitized (`/` → `_`).
- `unix.Flock(fd, LOCK_EX|LOCK_NB)`; on EWOULDBLOCK: exit 75 with message to stderr, unless `--wait` → blocking `LOCK_EX`.
- After acquiring: truncate + write own PID (informative only).
- Run child with inherited stdio, forward SIGINT/SIGTERM to child, propagate child's exit code (use `exec.ExitError.ExitCode()`; signal death → 128+sig).
- `--list`: iterate lock dir, for each file try `LOCK_EX|LOCK_NB`; if it fails, print `<name>\t<pid-from-file>`; if it succeeds, lock is stale (release + ignore).

**Tests (integration, via built binary):**
- [ ] Hold lock in process A (`alone --name t sleep 5`), B with same name exits 75 fast without running its command (marker file not created)
- [ ] SIGKILL A → B acquires immediately (flock semantics)
- [ ] `--wait`: B finishes after A finishes
- [ ] `alone -- sh -c "exit 42"` → exit 42
- [ ] Name derivation and sanitization unit tests

---

### Task 2: `notify` — command-completion notification

**Files:** Create `cmd/notify/main.go`, `cmd/notify/main_test.go`

**CLI contract:**
```
notify [--title T] [--sound] [--] CMD [ARGS...]
```
- Stop flag parsing at first non-flag arg (manual arg scan or `flag` + careful handling) so `notify ls --color` passes `--color` to `ls`; `--` always separates.
- Run child with inherited stdio; measure duration with `time.Since`.
- Notification via `godbus/dbus/v5` session bus, `org.freedesktop.Notifications.Notify`: success → `✓ <cmd> (<dur>)` urgency normal; failure → `✗ <cmd> failed (exit N, <dur>)` urgency critical (hint `urgency` byte 2), no expiry on critical.
- `--sound`: notification hint `sound-name: "message-new-instant"` (freedesktop sound naming).
- D-Bus unreachable → warn on stderr, still propagate exit code (tool must never break a pipeline).
- Propagate child's exit code.

**Tests:**
- [ ] Exit code propagation: `notify -- sh -c "exit 7"` → 7 (works headless: D-Bus failure only warns)
- [ ] Arg parsing unit tests: `notify --sound -- cmd --sound` → child gets `--sound`; `notify ls --color` → child gets `--color`
- [ ] Duration formatting via comum.FormatDuration
- [ ] Actual notification test: skip unless `DBUS_SESSION_BUS_ADDRESS` set

---

### Task 3: `waitfor` — wait for conditions

**Files:** Create `cmd/waitfor/main.go`, `cmd/waitfor/cond.go`, `cmd/waitfor/main_test.go`

**CLI contract:**
```
waitfor [--pid N] [--port [HOST:]PORT] [--exists PATH] [--gone PATH]
        [--file PATH --stable DUR] [--net] [--timeout DUR] [--interval DUR]
```
- Multiple conditions AND together (all goroutines must complete).
- `--timeout` → exit 124 (comum.ExitTimeout). `--interval` default 500ms for polled conditions.
- Mechanisms:
  - `--pid`: `unix.PidfdOpen(pid, 0)` + block on POLLIN via `unix.Poll` — no polling. ESRCH (already dead) → condition immediately true. Fallback to `kill(pid,0)` polling if pidfd unavailable.
  - `--port`: `net.DialTimeout("tcp", ...)` loop; bare port → localhost.
  - `--exists`/`--gone`: fsnotify watch on parent dir + initial check (avoid race: check after watch established). Parent dir missing for `--exists` → clear error exit 1, not panic.
  - `--file X --stable D`: fsnotify on file/parent; timer resets on each write event; fires when D elapses with no writes. Also stat-poll fallback comparing size+mtime.
  - `--net`: TCP connect loop to configurable target (default `1.1.1.1:443`, override via `--net-target`).

**Tests:**
- [ ] `--pid` of process that dies after 1s → returns after ~1s; nonexistent PID → immediate 0
- [ ] `--port` closed + `--timeout 2s` → exit 124 in ~2s; open a listener in test → returns 0
- [ ] `--exists`: touch file after delay → returns after touch; pre-existing → immediate
- [ ] `--gone`: remove file after delay
- [ ] `--stable`: append every 300ms for 1.5s, `--stable 1s` → returns ~1s after last write, not before
- [ ] Parent dir missing → stderr error, exit 1

---

### Task 4: `countdown` — terminal timer

**Files:** Create `cmd/countdown/main.go`, `cmd/countdown/main_test.go`

**CLI contract:**
```
countdown DURATION [--quiet] [--up]
```
- Duration via comum.ParseDuration (`25m`, `1h30m`, `90`, `1:30:00`). Parse error → clear stderr message, exit 2.
- TTY mode: `x/term` raw mode, single-line redraw (`\r\x1b[K`): progress bar + remaining + ETA wall-clock. Keys: space pause/resume, `q` cancel, `+`/`-` ±1min (floor 0). Restore terminal on all exits (defer + signal handler).
- Pause = accumulate elapsed-before-pause; wall clock keeps running.
- `--up`: count up to target.
- Non-TTY stdout or `--quiet`: no UI, just wait (interruptible), exit 0.
- SIGINT/`q` → restore terminal, exit 130.

**Tests:**
- [ ] Parser cases already in comum — add integration: `countdown 1s` piped (non-TTY) exits 0 in ~1s
- [ ] `countdown abc` → exit != 0, stderr mentions parse
- [ ] SIGINT during `countdown 10s --quiet` → exit 130
- [ ] Interactive keys: manual test only (documented in README)

---

### Task 5: `freshname` — next free filename

**Files:** Create `cmd/freshname/main.go`, `cmd/freshname/split.go`, `cmd/freshname/main_test.go`

**CLI contract:**
```
freshname [--create] [--sep S] [--start N] PATH
```
- Split name/ext with compound-extension list: `.tar.gz .tar.xz .tar.bz2 .tar.zst`. Dirs and extensionless files: suffix whole name. Hidden files (`.hidden`): leading dot is not an extension.
- No `--create`: if PATH free → print PATH; else try `name<sep>N.ext` for N = start(default 2), 3, ... print first free. Document race in --help.
- `--create`: loop `os.OpenFile(candidate, O_CREATE|O_EXCL|O_WRONLY, 0644)`; `ErrExist` → next N. Atomic, TOCTOU-free. Print winner.
- Output: the name only, to stdout, newline-terminated.

**Tests:**
- [ ] Unit: split of `a.txt`, `b.tar.gz`, `noext`, `.hidden`, `dir/` cases
- [ ] Tempdir sequence: free → same name; exists → `-2`; `-2` exists → `-3`; `b.tar.gz` → `b-2.tar.gz`; dir → `dir-2`
- [ ] `--create` creates file and prints name
- [ ] Concurrency: 20 goroutines `--create` same name → 20 unique names, 0 errors
- [ ] `--sep _` and `--start 1` respected

---

### Task 6: `recent` — recently modified files

**Files:** Create `cmd/recent/main.go`, `cmd/recent/walk.go`, `cmd/recent/main_test.go`

**CLI contract:**
```
recent [WINDOW] [DIR] [--created] [--all] [--dirs] [-0] [--top N]
```
- Defaults: window 10m, dir `~`, top 20.
- Walk: `filepath.WalkDir` with worker pool over subdirs (or plain WalkDir — correctness first, parallelism if simple). Prune excluded dirs before descending (`fs.SkipDir`).
- Default exclusions: `.cache .git node_modules target .local/share/Trash .venv __pycache__`, hidden lock/state files (`.*.swp`, `*~`), browser cache dirs. Config: `~/.config/spare-tools/recent.toml`, simple `exclude = ["..."]` line-based parse (no toml dep — parse minimal subset).
- `--created`: btime via `unix.Statx` (`STATX_BTIME`); unsupported FS → warn once to stderr, use mtime.
- Filter by window during walk. Sort by mtime desc, print top N.
- TTY: `2m ago   1.4M  ~/Downloads/x.pdf` (relative time, human size, colors). Pipe: raw stable format. `-0`: NUL-terminated paths only.

**Tests:**
- [ ] Tempdir: files with controlled mtimes (`os.Chtimes`) → window inclusion/exclusion
- [ ] `node_modules/x` hidden by default, appears with `--all`
- [ ] Order: 3 files distinct mtimes → newest first
- [ ] `-0` with spaces/newlines in names → intact paths
- [ ] `--dirs` includes directories

---

### Task 7: Integration & ship (main session)

- [ ] `go vet ./... && go test ./... && gofmt -l .` clean
- [ ] `./install.sh all` to temp BINDIR, smoke-test each binary `--help`
- [ ] Manual smoke: freshname sequence, countdown 1s quiet, waitfor --port timeout, alone exit-75
- [ ] `gh repo create ThiagoAVicente/spare-tools --public`, push main
