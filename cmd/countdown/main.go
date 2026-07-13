// Command countdown is a visible terminal timer built to compose:
//
//	countdown 25m && notify-send pausa
//
// In a TTY it draws a single-line progress bar with the remaining time and
// ETA. Interactive keys (manual-test only, not covered by automated tests):
//
//	space  pause/resume (shows a ⏸ indicator; pausing extends the ETA)
//	q      cancel, exit 130
//	+      add one minute
//	-      subtract one minute (remaining is floored at 0, which ends
//	       the timer normally)
//	Ctrl+C cancel, exit 130
//
// When stdout is not a terminal (or with --quiet) it just sleeps for the
// duration with no output, exiting 0 at the end or 130 on SIGINT/SIGTERM.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ThiagoAVicente/spare-tools/internal/comum"
	"golang.org/x/term"
)

const (
	barWidth  = 30
	usageText = "usage: countdown DURATION [--quiet] [--up]"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var quiet, up bool
	var durArg string

	for _, a := range args {
		switch a {
		case "--quiet", "-q":
			quiet = true
		case "--up":
			up = true
		case "--help", "-h":
			fmt.Println(usageText)
			fmt.Println("\nDURATION accepts \"25m\", \"1h30m\", \"90\" (seconds) or \"1:30:00\".")
			fmt.Println("Keys (TTY mode): space pause/resume, q cancel, + add 1min, - sub 1min.")
			return comum.ExitOK
		default:
			if len(a) > 1 && a[0] == '-' && !(a[1] >= '0' && a[1] <= '9') {
				fmt.Fprintf(os.Stderr, "countdown: unknown flag %q\n%s\n", a, usageText)
				return 2
			}
			if durArg != "" {
				fmt.Fprintf(os.Stderr, "countdown: unexpected argument %q\n%s\n", a, usageText)
				return 2
			}
			durArg = a
		}
	}

	if durArg == "" {
		fmt.Fprintf(os.Stderr, "countdown: missing duration\n%s\n", usageText)
		return 2
	}

	d, err := comum.ParseDuration(durArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "countdown: invalid duration %q\n", durArg)
		return 2
	}

	if quiet || !comum.StdoutIsTTY() {
		return runQuiet(d)
	}
	return runTTY(d, up)
}

// runQuiet waits for the duration with no UI, interruptible by signals.
func runQuiet(d time.Duration) int {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)

	select {
	case <-time.After(d):
		return comum.ExitOK
	case <-sig:
		return comum.ExitCanceled
	}
}

// runTTY draws the interactive single-line timer, redrawing at ~10Hz.
func runTTY(target time.Duration, up bool) int {
	fd := int(os.Stdin.Fd())
	oldState, rawErr := term.MakeRaw(fd)
	restore := func() {
		if rawErr == nil {
			term.Restore(fd, oldState)
		}
	}
	// Restore terminal state and leave the shell prompt on a clean line,
	// whatever the exit path.
	finish := func(code int) int {
		fmt.Print("\r\x1b[K")
		restore()
		fmt.Println()
		return code
	}

	// SIGTERM always arrives as a signal; SIGINT only on the non-raw path
	// (in raw mode Ctrl+C arrives as byte 0x03 on stdin instead).
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)

	keys := make(chan byte)
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				return
			}
			if n == 1 {
				keys <- buf[0]
			}
		}
	}()

	// Pause semantics: accumulate elapsed across resume periods rather
	// than tracking a wall-clock deadline, so pausing extends the end.
	var elapsed time.Duration
	last := time.Now()
	paused := false

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	draw := func() {
		remaining := target - elapsed
		if remaining < 0 {
			remaining = 0
		}
		frac := 1.0
		if target > 0 {
			frac = float64(elapsed) / float64(target)
		}
		var shown time.Duration
		if up {
			shown = elapsed
			if shown > target {
				shown = target
			}
		} else {
			shown = remaining
			frac = 1 - frac // countdown bar drains; --up bar fills
		}
		line := fmt.Sprintf("\r\x1b[K%s %s  ETA %s",
			renderBar(frac, barWidth),
			formatClock(shown),
			time.Now().Add(remaining).Format("15:04:05"))
		if paused {
			line += " ⏸"
		}
		fmt.Print(line)
	}

	draw()
	for {
		select {
		case <-sig:
			return finish(comum.ExitCanceled)

		case k := <-keys:
			switch k {
			case 0x03, 'q': // Ctrl+C in raw mode, or q
				return finish(comum.ExitCanceled)
			case ' ':
				if paused {
					last = time.Now() // don't count the paused gap
				}
				paused = !paused
				draw()
			case '+':
				target += time.Minute
				draw()
			case '-':
				target -= time.Minute
				if target < 0 {
					target = 0
				}
				if elapsed >= target {
					// Remaining floored at 0: end normally.
					return finish(comum.ExitOK)
				}
				draw()
			}

		case now := <-ticker.C:
			if !paused {
				elapsed += now.Sub(last)
			}
			last = now
			if !paused && elapsed >= target {
				return finish(comum.ExitOK)
			}
			draw()
		}
	}
}
