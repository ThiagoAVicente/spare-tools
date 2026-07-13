# spare-tools

> tools nobody asked for. built in spare time, kept as spare parts.

Six small Go CLI tools. Each does one thing, composes with pipes and `&&`,
exits with an honest code (0 = success), and has a decent `--help`.

| Tool | What it does |
|---|---|
| [`alone`](#alone) | run a command guaranteeing a single instance (flock-based) |
| [`notify`](#notify) | desktop notification when a command finishes |
| [`waitfor`](#waitfor) | block until a condition is true (pid, port, file, net) |
| [`countdown`](#countdown) | visible terminal timer, made to compose with `&&` |
| [`freshname`](#freshname) | next free filename (`report-2.pdf`), race-free with `--create` |
| [`recent`](#recent) | recently modified files, sane exclusions, newest first |

## Install

Requires a Go toolchain.

```sh
./install.sh countdown     # one tool
./install.sh all           # everything
```

Installs to `~/.local/bin` (override with `BINDIR=...`). Re-running the
script updates (overwrites) the installed binary.

## Exit code convention

| Code | Meaning |
|---|---|
| 0 | success |
| 75 | lock busy (`alone`, EX_TEMPFAIL) |
| 124 | timeout (`waitfor --timeout`, same as `timeout(1)`) |
| 130 | canceled by user (Ctrl+C) |

Tools that wrap a command (`alone`, `notify`) propagate the child's exit code.

## alone

Run a command guaranteeing only one instance exists at a time.
Kernel `flock(2)` on a file in `$XDG_RUNTIME_DIR` — the lock is released
automatically when the process dies, even with `kill -9`.

```sh
alone backup.sh                    # if one is already running, exit 75
alone --wait backup.sh             # wait for the other one, then run
alone --name sync rsync -a a/ b/   # explicit lock name
alone --list                       # show active locks and PIDs
```

## notify

Run a command; when it ends, fire a desktop notification (D-Bus) with the
result and duration. Green/normal on success, red/critical on failure.

```sh
notify cargo build --release       # "✓ cargo build (2m14s)"
notify -- make -j8                 # -- separates tool flags from the command
notify --sound make test
notify --title "Backup" ./backup.sh
```

## waitfor

Block until a condition is true, then exit 0. Conditions AND together.
`--timeout` exits 124.

```sh
waitfor --pid 4223 && notify-send "build done"     # pidfd, no polling
waitfor --port 5432 && psql                        # port accepting connections
waitfor --port localhost:8080 --timeout 30s
waitfor --file ~/download.iso --stable 5s          # file stopped growing
waitfor --net && git push                          # internet is back
waitfor --exists /tmp/ready.flag
waitfor --gone /var/run/app.pid
```

## countdown

Terminal timer that exits 0 at the end.

```sh
countdown 25m && notify-send "break"
countdown 1h30m                    # composed durations
countdown 90                       # bare seconds
countdown 10m --quiet              # no UI, composable sleep
countdown 5m --up                  # stopwatch up to 5m
```

Keys: `space` pause/resume, `q`/Ctrl+C cancel (exit 130), `+`/`-` ±1min.
Falls back to quiet mode automatically when stdout is not a TTY.

## freshname

Print the first filename that does **not** exist, following `name-N.ext`.

```sh
freshname report.pdf               # exists? -> "report-2.pdf"
freshname --create report.pdf      # atomically create it (O_CREAT|O_EXCL), race-free
freshname --sep _ photo.jpg        # "photo_2.jpg"
freshname --start 1 photo.jpg      # start at "photo-1.jpg"
cp data.csv "$(freshname backup/data.csv)"
```

Knows compound extensions: `archive.tar.gz` → `archive-2.tar.gz`.

## recent

List recently modified files, newest first, with sane default exclusions
(`.git`, `node_modules`, `.cache`, `target`, ...).

```sh
recent                        # last 10 min from ~, top 20
recent 1h                     # 1-hour window
recent 30m ~/Downloads
recent 1h --created           # only new files (btime)
recent 2h --all               # disable exclusions
recent 1h -0 | xargs -0 ls -la
```

Exclusions configurable in `~/.config/spare-tools/recent.toml`.

## Development

```sh
go test ./...
go vet ./...
```

Tests that need a desktop/D-Bus session are skipped automatically in
headless environments.
