// Command recent lists files modified (or created) recently, newest first,
// with sane default exclusions. It answers "where did the file I just
// saved/downloaded go?".
//
// Usage:
//
//	recent [WINDOW] [DIR] [--created] [--all] [--dirs] [-0] [--top N]
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ThiagoAVicente/spare-tools/internal/comum"
)

type options struct {
	window   time.Duration
	dir      string
	created  bool
	all      bool
	dirs     bool
	nul      bool
	top      int
	excludes []string
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: recent [WINDOW] [DIR] [--created] [--all] [--dirs] [-0] [--top N]

Lists files modified in the last WINDOW (default 10m) under DIR (default $HOME),
newest first. WINDOW and DIR may be given in either order.

  --created   filter/sort by creation time (btime) instead of mtime
  --all       disable all exclusions
  --dirs      include directories in the results
  -0          print NUL-terminated paths only
  --top N     show at most N entries (default 20)`)
}

func parseArgs(args []string, home string) (options, error) {
	opt := options{window: 10 * time.Minute, dir: home, top: 20}
	windowSet, dirSet := false, false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--created":
			opt.created = true
		case a == "--all":
			opt.all = true
		case a == "--dirs":
			opt.dirs = true
		case a == "-0":
			opt.nul = true
		case a == "--top" || strings.HasPrefix(a, "--top="):
			var v string
			if eq := strings.IndexByte(a, '='); eq >= 0 {
				v = a[eq+1:]
			} else {
				i++
				if i >= len(args) {
					return opt, fmt.Errorf("--top requires a number")
				}
				v = args[i]
			}
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return opt, fmt.Errorf("invalid --top value %q", v)
			}
			opt.top = n
		case a == "-h" || a == "--help":
			usage()
			os.Exit(0)
		case strings.HasPrefix(a, "-") && a != "-":
			return opt, fmt.Errorf("unknown flag %q", a)
		default:
			// Positional: WINDOW (parseable duration) or DIR (existing path),
			// in either order.
			if !windowSet {
				if d, err := comum.ParseDuration(a); err == nil {
					opt.window = d
					windowSet = true
					continue
				}
			}
			if !dirSet {
				if info, err := os.Stat(a); err == nil && info.IsDir() {
					opt.dir = a
					dirSet = true
					continue
				}
			}
			return opt, fmt.Errorf("argument %q is neither a duration nor an existing directory", a)
		}
	}
	return opt, nil
}

// parseExcludeLine parses a config line of the form `exclude = ["a", "b"]`.
// The second return value is false when the line is not a well-formed
// exclude assignment.
func parseExcludeLine(line string) ([]string, bool) {
	eq := strings.IndexByte(line, '=')
	if eq < 0 || strings.TrimSpace(line[:eq]) != "exclude" {
		return nil, false
	}
	v := strings.TrimSpace(line[eq+1:])
	if len(v) < 2 || v[0] != '[' || v[len(v)-1] != ']' {
		return nil, false
	}
	inner := strings.TrimSpace(v[1 : len(v)-1])
	if inner == "" {
		return []string{}, true
	}
	out := []string{}
	for _, part := range strings.Split(inner, ",") {
		part = strings.TrimSpace(part)
		if len(part) < 2 || (part[0] != '"' && part[0] != '\'') || part[len(part)-1] != part[0] {
			return nil, false
		}
		out = append(out, part[1:len(part)-1])
	}
	return out, true
}

// loadExcludes reads ~/.config/spare-tools/recent.toml (minimal subset, no
// toml dep). A well-formed `exclude = [...]` line replaces the defaults;
// a malformed one warns on stderr and keeps the defaults.
func loadExcludes() []string {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return defaultExcludes
	}
	data, err := os.ReadFile(filepath.Join(cfgDir, "spare-tools", "recent.toml"))
	if err != nil {
		return defaultExcludes
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "exclude") {
			continue
		}
		if ex, ok := parseExcludeLine(line); ok {
			return ex
		}
		fmt.Fprintf(os.Stderr, "recent: malformed exclude line in recent.toml, using defaults: %s\n", line)
		return defaultExcludes
	}
	return defaultExcludes
}

// humanSize renders a byte count compactly: "500", "1.4M", "10M", "2.0G".
// Values below 10 in their unit keep one decimal.
func humanSize(b int64) string {
	const k = 1024.0
	f := float64(b)
	if f < k {
		return strconv.FormatInt(b, 10)
	}
	for _, u := range []string{"K", "M", "G", "T"} {
		f /= k
		if f < k || u == "T" {
			if f < 10 {
				return fmt.Sprintf("%.1f%s", f, u)
			}
			return fmt.Sprintf("%.0f%s", f, u)
		}
	}
	return "" // unreachable
}

// relTime renders an age compactly: "30s ago", "2m ago", "3h ago", "1d ago".
func relTime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// tildify abbreviates $HOME to ~ in a path.
func tildify(p, home string) string {
	if home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

func main() {
	home, _ := os.UserHomeDir()
	opt, err := parseArgs(os.Args[1:], home)
	if err != nil {
		fmt.Fprintln(os.Stderr, "recent:", err)
		usage()
		os.Exit(2)
	}
	if opt.dir == "" {
		fmt.Fprintln(os.Stderr, "recent: no directory given and $HOME is unset")
		os.Exit(2)
	}
	if !opt.all {
		opt.excludes = loadExcludes()
	}

	now := time.Now()
	entries := collect(opt.dir, now.Add(-opt.window), opt)

	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].t.Equal(entries[j].t) {
			return entries[i].t.After(entries[j].t)
		}
		return entries[i].path < entries[j].path
	})
	if len(entries) > opt.top {
		entries = entries[:opt.top]
	}

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	switch {
	case opt.nul:
		for _, e := range entries {
			w.WriteString(e.path)
			w.WriteByte(0)
		}
	case comum.StdoutIsTTY():
		for _, e := range entries {
			fmt.Fprintf(w, "\x1b[36m%-8s\x1b[0m %5s  %s\n",
				relTime(now.Sub(e.t)), humanSize(e.size), tildify(e.path, home))
		}
	default:
		for _, e := range entries {
			fmt.Fprintf(w, "%d\t%d\t%s\n", e.t.Unix(), e.size, e.path)
		}
	}
}
