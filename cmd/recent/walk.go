package main

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// entry is one candidate result found during the walk.
type entry struct {
	path  string
	t     time.Time
	size  int64
	isDir bool
}

// defaultExcludes are directory names pruned before descending. An entry
// containing "/" is matched as a path suffix instead of a base name.
var defaultExcludes = []string{
	".cache",
	".git",
	"node_modules",
	"target",
	".venv",
	"__pycache__",
	".local/share/Trash",
}

// isExcludedDir reports whether a directory should be pruned. Patterns
// without a slash match the base name at any depth; patterns with a slash
// match as a path suffix (e.g. ".local/share/Trash").
func isExcludedDir(p, base string, excludes []string) bool {
	slashed := filepath.ToSlash(p)
	for _, e := range excludes {
		if strings.Contains(e, "/") {
			if slashed == e || strings.HasSuffix(slashed, "/"+e) {
				return true
			}
		} else if base == e {
			return true
		}
	}
	return false
}

// isExcludedFile reports whether a file is hidden editor state (".*.swp", "*~").
func isExcludedFile(name string) bool {
	if strings.HasSuffix(name, "~") {
		return true
	}
	if ok, _ := path.Match(".*.swp", name); ok {
		return true
	}
	return false
}

var btimeWarnOnce sync.Once

// birthTime returns the file creation time via statx, falling back to the
// given mtime (with a single stderr warning) when the filesystem does not
// report btime.
func birthTime(p string, fallback time.Time) time.Time {
	var stx unix.Statx_t
	err := unix.Statx(unix.AT_FDCWD, p, unix.AT_SYMLINK_NOFOLLOW, unix.STATX_BTIME, &stx)
	if err != nil || stx.Mask&unix.STATX_BTIME == 0 {
		btimeWarnOnce.Do(func() {
			fmt.Fprintln(os.Stderr, "recent: filesystem does not report creation time, falling back to mtime")
		})
		return fallback
	}
	return time.Unix(stx.Btime.Sec, int64(stx.Btime.Nsec))
}

// collect walks root, pruning excluded directories before descending, and
// returns entries whose time is within the window (>= cutoff). Unreadable
// dirs and files are skipped silently.
func collect(root string, cutoff time.Time, opt options) []entry {
	var out []entry
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // permission or race errors must not abort the walk
		}
		if d.IsDir() {
			if p == root {
				return nil
			}
			if !opt.all && isExcludedDir(p, d.Name(), opt.excludes) {
				return fs.SkipDir
			}
			if !opt.dirs {
				return nil
			}
		} else {
			if !d.Type().IsRegular() {
				return nil
			}
			if !opt.all && isExcludedFile(d.Name()) {
				return nil
			}
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		t := info.ModTime()
		if opt.created {
			t = birthTime(p, t)
		}
		if t.Before(cutoff) {
			return nil
		}
		out = append(out, entry{path: p, t: t, size: info.Size(), isDir: d.IsDir()})
		return nil
	})
	return out
}
