package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/sys/unix"
)

// condFunc blocks until its condition is true (nil) or fails (non-nil).
// A context error means the wait was cut short (timeout/cancel).
type condFunc func(ctx context.Context) error

// sleepCtx waits d or until ctx is done, whichever comes first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// waitPid blocks until the process with the given pid exits.
// Uses pidfd + poll(2) (no polling loop, no parent relationship needed);
// falls back to kill(pid, 0) polling on old kernels or EPERM.
func waitPid(pid int, interval time.Duration) condFunc {
	return func(ctx context.Context) error {
		fd, err := unix.PidfdOpen(pid, 0)
		if err == unix.ESRCH {
			return nil // already dead
		}
		if err != nil {
			// pidfd unavailable (old kernel, EPERM, ...): poll kill(0).
			return pollPid(ctx, pid, interval)
		}
		defer unix.Close(fd)

		done := make(chan error, 1)
		go func() {
			fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
			for {
				_, err := unix.Poll(fds, -1)
				if err == unix.EINTR {
					continue
				}
				done <- err
				return
			}
		}()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-done:
			return err
		}
	}
}

func pollPid(ctx context.Context, pid int, interval time.Duration) error {
	for {
		err := unix.Kill(pid, 0)
		if err == unix.ESRCH {
			return nil
		}
		if err != nil && err != unix.EPERM {
			return fmt.Errorf("pid %d: %w", pid, err)
		}
		if err := sleepCtx(ctx, interval); err != nil {
			return err
		}
	}
}

// waitDial blocks until a TCP connection to addr succeeds.
func waitDial(addr string, interval time.Duration) condFunc {
	return func(ctx context.Context) error {
		for {
			conn, err := net.DialTimeout("tcp", addr, time.Second)
			if err == nil {
				conn.Close()
				return nil
			}
			if err := sleepCtx(ctx, interval); err != nil {
				return err
			}
		}
	}
}

// normalizePort turns "8080" into "localhost:8080"; "host:port" passes through.
func normalizePort(s string) string {
	if strings.Contains(s, ":") {
		return s
	}
	return "localhost:" + s
}

// waitPath blocks until path exists (gone=false) or stops existing (gone=true).
// Watches the parent directory with fsnotify, with the initial stat done after
// the watch is established so nothing slips through the race window.
func waitPath(path string, gone bool, interval time.Duration) condFunc {
	return func(ctx context.Context) error {
		abs, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		dir := filepath.Dir(abs)

		watcher, werr := fsnotify.NewWatcher()
		if werr == nil {
			defer watcher.Close()
			werr = watcher.Add(dir)
		}
		if werr != nil {
			// Parent dir missing or watcher unavailable.
			if _, err := os.Stat(dir); err != nil {
				if gone {
					return nil // parent gone => file gone
				}
				return fmt.Errorf("cannot watch %s: parent directory: %v", path, err)
			}
			// Watcher failed for some other reason: fall back to polling.
			return pollPath(ctx, abs, gone, interval)
		}

		check := func() bool {
			_, err := os.Stat(abs)
			exists := err == nil
			return exists != gone
		}
		if check() {
			return nil
		}
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case ev, ok := <-watcher.Events:
				if !ok {
					return pollPath(ctx, abs, gone, interval)
				}
				if filepath.Clean(ev.Name) == abs && check() {
					return nil
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return pollPath(ctx, abs, gone, interval)
				}
				// Non-fatal watcher error: re-check and keep going.
				if check() {
					return nil
				}
			}
		}
	}
}

func pollPath(ctx context.Context, abs string, gone bool, interval time.Duration) error {
	for {
		_, err := os.Stat(abs)
		exists := err == nil
		if exists != gone {
			return nil
		}
		if err := sleepCtx(ctx, interval); err != nil {
			return err
		}
	}
}

// waitStable blocks until the file at path has stopped changing for the
// given duration. fsnotify events on the path reset the quiet timer; if
// fsnotify is unavailable it degrades to stat polling (size+mtime).
func waitStable(path string, stable, interval time.Duration) condFunc {
	return func(ctx context.Context) error {
		abs, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		dir := filepath.Dir(abs)

		watcher, werr := fsnotify.NewWatcher()
		if werr == nil {
			defer watcher.Close()
			// Watch the parent so we also see the file appear.
			werr = watcher.Add(dir)
		}
		if werr != nil {
			return pollStable(ctx, abs, stable, interval)
		}

		timer := time.NewTimer(stable)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case ev, ok := <-watcher.Events:
				if !ok {
					return pollStable(ctx, abs, stable, interval)
				}
				if filepath.Clean(ev.Name) == abs {
					// Any activity on the file resets the quiet period.
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					timer.Reset(stable)
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return pollStable(ctx, abs, stable, interval)
				}
			case <-timer.C:
				// Quiet for the full duration; only counts if the file exists.
				if _, err := os.Stat(abs); err == nil {
					return nil
				}
				timer.Reset(stable)
			}
		}
	}
}

func pollStable(ctx context.Context, abs string, stable, interval time.Duration) error {
	var lastSize int64 = -1
	var lastMod time.Time
	lastChange := time.Now()
	for {
		fi, err := os.Stat(abs)
		if err == nil {
			if fi.Size() != lastSize || !fi.ModTime().Equal(lastMod) {
				lastSize, lastMod = fi.Size(), fi.ModTime()
				lastChange = time.Now()
			} else if time.Since(lastChange) >= stable {
				return nil
			}
		} else {
			lastSize, lastChange = -1, time.Now()
		}
		if err := sleepCtx(ctx, interval); err != nil {
			return err
		}
	}
}
