// Command notify runs a command and fires a desktop notification when it
// finishes, reporting success or failure and how long it took.
//
// The tool is transparent: the child inherits stdin/stdout/stderr and its
// exit code is propagated exactly, so notify can be dropped in front of any
// command in a pipeline without changing behavior.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ThiagoAVicente/spare-tools/internal/comum"
)

const usage = `notify - run a command, get a desktop notification when it finishes

Usage:
  notify [--title T] [--sound] [--] CMD [ARGS...]

Flags:
  --title T   Use T as the notification summary (result goes in the body)
  --sound     Play a sound with the notification
  -h, --help  Show this help

Flag parsing stops at the first non-flag argument, so flags meant for the
child command need no separator. Use "--" to be explicit when the child
command name looks like a notify flag.

Exit code: the child's exit code is propagated exactly (128+signal if the
child was killed by a signal); 127 if the command was not found. If the
D-Bus session bus is unreachable, a warning is printed to stderr and the
child's exit code is still returned.

Examples:
  notify make -j8                     Notify when the build finishes
  notify --title "Backup" ./backup.sh Custom notification title
  notify --sound -- cargo test        Play a sound; child gets no flags
  notify sleep 300 && echo done       Transparent in pipelines
`

// options holds the tool's own flags, parsed before the child command.
type options struct {
	title string
	sound bool
	help  bool
}

// parseArgs scans args, consuming known notify flags until the first
// non-flag token or "--". Everything from there on is the child command.
func parseArgs(args []string) (options, []string, error) {
	var opts options
	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--":
			return opts, args[i+1:], nil
		case arg == "--sound":
			opts.sound = true
			i++
		case arg == "--title":
			if i+1 >= len(args) {
				return opts, nil, fmt.Errorf("--title requires a value")
			}
			opts.title = args[i+1]
			i += 2
		case strings.HasPrefix(arg, "--title="):
			opts.title = strings.TrimPrefix(arg, "--title=")
			i++
		case arg == "-h" || arg == "--help":
			opts.help = true
			i++
		default:
			// First unknown token: the child command starts here.
			return opts, args[i:], nil
		}
	}
	return opts, nil, nil
}

func main() {
	os.Exit(run())
}

func run() int {
	opts, cmdArgs, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "notify: %v\n", err)
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	if opts.help {
		fmt.Print(usage)
		return comum.ExitOK
	}
	if len(cmdArgs) == 0 {
		fmt.Fprint(os.Stderr, "notify: no command given\n")
		fmt.Fprint(os.Stderr, usage)
		return 2
	}

	child := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr

	start := time.Now()
	if err := child.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "notify: %v\n", err)
		return 127
	}

	// Forward SIGINT/SIGTERM to the child; it decides how to die.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-sigCh:
				_ = child.Process.Signal(sig)
			case <-done:
				return
			}
		}
	}()

	waitErr := child.Wait()
	close(done)
	signal.Stop(sigCh)
	dur := time.Since(start)

	code := exitCode(waitErr)
	if err := sendNotification(opts, cmdArgs[0], code, dur); err != nil {
		fmt.Fprintf(os.Stderr, "notify: could not send notification: %v\n", err)
	}
	return code
}

// exitCode extracts the child's exit code, mapping signal death to 128+sig.
func exitCode(waitErr error) int {
	if waitErr == nil {
		return comum.ExitOK
	}
	ee, ok := waitErr.(*exec.ExitError)
	if !ok {
		return 1
	}
	if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ee.ExitCode()
}
