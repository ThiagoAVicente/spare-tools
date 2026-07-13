// Command alone runs a command guaranteeing only one instance at a time.
//
// It takes an exclusive flock(2) on a per-name lock file before running the
// command. Because the kernel releases the lock automatically when the
// holding process dies — even on SIGKILL — there are no stale locks to clean
// up.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/ThiagoAVicente/spare-tools/internal/comum"
	"golang.org/x/sys/unix"
)

func usage() {
	fmt.Fprintf(os.Stderr, `alone - run a command guaranteeing only one instance at a time

Usage:
  alone [--wait] [--name NAME] [--] CMD [ARGS...]
  alone --list

Options:
  --name NAME   lock name (default: basename of CMD)
  --wait        wait for the lock instead of failing when busy
  --list        list currently held locks (name and pid)
  -h, --help    show this help

Exit status:
  the command's exit status (128+signal if it was killed by a signal)
  %d  another instance already holds the lock (only without --wait)

Locks live in $XDG_RUNTIME_DIR/alone/ and are backed by flock(2), so the
kernel releases them automatically when the holder dies, even on kill -9.

Examples:
  alone backup.sh                    # skip if backup.sh is already running
  alone --wait --name db pg_dump db  # queue behind the current holder
  alone --name sync -- rsync -a a b  # use -- before commands with flags
  alone --list                       # show live locks
`, comum.ExitTempFail)
}

// deriveName returns the default lock name for a command: its basename.
func deriveName(cmd string) string {
	return sanitizeName(filepath.Base(cmd))
}

// sanitizeName makes a lock name safe to use as a file name.
func sanitizeName(name string) string {
	return strings.ReplaceAll(name, "/", "_")
}

// lockDir returns the directory holding lock files, creating it 0700.
func lockDir() (string, error) {
	var dir string
	if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" {
		dir = filepath.Join(rt, "alone")
	} else {
		dir = filepath.Join(os.TempDir(), fmt.Sprintf("alone-%d", os.Getuid()))
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// acquire opens the lock file for name and takes an exclusive flock on it.
// With wait=false a busy lock returns (nil, unix.EWOULDBLOCK). The returned
// file must stay open for as long as the lock should be held.
func acquire(dir, name string, wait bool) (*os.File, error) {
	path := filepath.Join(dir, name+".lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	how := unix.LOCK_EX
	if !wait {
		how |= unix.LOCK_NB
	}
	if err := unix.Flock(int(f.Fd()), how); err != nil {
		f.Close()
		return nil, err
	}
	// Informative only (for --list); the flock is the real lock.
	if err := f.Truncate(0); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.WriteAt([]byte(fmt.Sprintf("%d\n", os.Getpid())), 0); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// list prints "name\tpid" for every live lock in dir.
func list(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".lock") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		f, err := os.Open(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			// Stale: nobody holds it. Unlock and skip.
			unix.Flock(int(f.Fd()), unix.LOCK_UN)
			f.Close()
			continue
		}
		buf := make([]byte, 32)
		k, _ := f.Read(buf)
		f.Close()
		pid := strings.TrimSpace(string(buf[:k]))
		fmt.Printf("%s\t%s\n", strings.TrimSuffix(n, ".lock"), pid)
	}
	return nil
}

// run executes argv with inherited stdio, forwarding SIGINT/SIGTERM, and
// returns the exit code to propagate.
func run(argv []string) int {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigc)

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "alone: %v\n", err)
		return 127
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-sigc:
				cmd.Process.Signal(sig)
			case <-done:
				return
			}
		}
	}()
	err := cmd.Wait()
	close(done)

	if err == nil {
		return comum.ExitOK
	}
	if ee, ok := err.(*exec.ExitError); ok {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return ee.ExitCode()
	}
	fmt.Fprintf(os.Stderr, "alone: %v\n", err)
	return 127
}

func main() {
	fs := flag.NewFlagSet("alone", flag.ExitOnError)
	fs.Usage = usage
	wait := fs.Bool("wait", false, "wait for the lock instead of failing when busy")
	name := fs.String("name", "", "lock name (default: basename of CMD)")
	doList := fs.Bool("list", false, "list currently held locks")
	fs.Parse(os.Args[1:]) // stops at the first non-flag arg; honors "--"

	dir, err := lockDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "alone: %v\n", err)
		os.Exit(1)
	}

	if *doList {
		if err := list(dir); err != nil {
			fmt.Fprintf(os.Stderr, "alone: %v\n", err)
			os.Exit(1)
		}
		return
	}

	argv := fs.Args()
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "alone: no command given")
		usage()
		os.Exit(2)
	}

	lockName := *name
	if lockName == "" {
		lockName = deriveName(argv[0])
	} else {
		lockName = sanitizeName(lockName)
	}

	f, err := acquire(dir, lockName, *wait)
	if err != nil {
		if err == unix.EWOULDBLOCK {
			fmt.Fprintf(os.Stderr, "alone: %q is already running (lock %s held); use --wait to queue\n", lockName, filepath.Join(dir, lockName+".lock"))
			os.Exit(comum.ExitTempFail)
		}
		fmt.Fprintf(os.Stderr, "alone: %v\n", err)
		os.Exit(1)
	}
	defer f.Close() // keep the fd (and thus the lock) open until exit

	os.Exit(run(argv))
}
