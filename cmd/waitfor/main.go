// Command waitfor blocks until one or more conditions become true, then
// exits 0. Conditions combine with AND. With --timeout it exits 124 on
// deadline (timeout(1) convention); SIGINT exits 130.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/ThiagoAVicente/spare-tools/internal/comum"
)

const usageText = `usage: waitfor [--pid N] [--port [HOST:]PORT] [--exists PATH] [--gone PATH]
               [--file PATH --stable DUR] [--net] [--net-target HOST:PORT]
               [--timeout DUR] [--interval DUR]

Blocks until every given condition is true, then exits 0.

  --pid N              process N has exited
  --port [HOST:]PORT   TCP connect to PORT succeeds (default host: localhost)
  --exists PATH        PATH exists
  --gone PATH          PATH no longer exists
  --file PATH          with --stable: PATH stopped changing for DUR
  --stable DUR         quiet period for --file
  --net                network is reachable (TCP connect to --net-target)
  --net-target ADDR    target for --net (default 1.1.1.1:443)
  --timeout DUR        give up after DUR, exit 124
  --interval DUR       polling interval where polling is needed (default 500ms)
`

func usageErr(msg string) {
	fmt.Fprintf(os.Stderr, "waitfor: %s\n\n%s", msg, usageText)
	os.Exit(2)
}

func main() {
	fs := flag.NewFlagSet("waitfor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usageText) }

	pid := fs.Int("pid", 0, "")
	port := fs.String("port", "", "")
	exists := fs.String("exists", "", "")
	gone := fs.String("gone", "", "")
	file := fs.String("file", "", "")
	stableStr := fs.String("stable", "", "")
	netFlag := fs.Bool("net", false, "")
	netTarget := fs.String("net-target", "1.1.1.1:443", "")
	timeoutStr := fs.String("timeout", "", "")
	intervalStr := fs.String("interval", "", "")

	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0) // fs.Usage already printed the help text
		}
		os.Exit(2)
	}
	if fs.NArg() > 0 {
		usageErr(fmt.Sprintf("unexpected argument %q", fs.Arg(0)))
	}

	interval := 500 * time.Millisecond
	if *intervalStr != "" {
		d, err := comum.ParseDuration(*intervalStr)
		if err != nil || d <= 0 {
			usageErr(fmt.Sprintf("invalid --interval %q", *intervalStr))
		}
		interval = d
	}

	var timeout time.Duration
	if *timeoutStr != "" {
		d, err := comum.ParseDuration(*timeoutStr)
		if err != nil || d <= 0 {
			usageErr(fmt.Sprintf("invalid --timeout %q", *timeoutStr))
		}
		timeout = d
	}

	if (*file == "") != (*stableStr == "") {
		usageErr("--file and --stable must be given together")
	}

	var conds []condFunc
	if *pid != 0 {
		if *pid < 0 {
			usageErr(fmt.Sprintf("invalid --pid %d", *pid))
		}
		conds = append(conds, waitPid(*pid, interval))
	}
	if *port != "" {
		conds = append(conds, waitDial(normalizePort(*port), interval))
	}
	if *exists != "" {
		conds = append(conds, waitPath(*exists, false, interval))
	}
	if *gone != "" {
		conds = append(conds, waitPath(*gone, true, interval))
	}
	if *file != "" {
		stable, err := comum.ParseDuration(*stableStr)
		if err != nil || stable <= 0 {
			usageErr(fmt.Sprintf("invalid --stable %q", *stableStr))
		}
		conds = append(conds, waitStable(*file, stable, interval))
	}
	if *netFlag {
		conds = append(conds, waitDial(*netTarget, interval))
	}
	if len(conds) == 0 {
		usageErr("at least one condition required")
	}

	ctx := context.Background()
	var cancel context.CancelFunc = func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	errCh := make(chan error, len(conds))
	done := make(chan struct{})
	var wg sync.WaitGroup
	for _, c := range conds {
		wg.Add(1)
		go func(c condFunc) {
			defer wg.Done()
			if err := c(ctx); err != nil {
				errCh <- err
			}
		}(c)
	}
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All conditions satisfied; a failed condition would have hit errCh
		// first because its error stays buffered.
		select {
		case err := <-errCh:
			exitErr(err)
		default:
			os.Exit(comum.ExitOK)
		}
	case err := <-errCh:
		exitErr(err)
	case <-ctx.Done():
		os.Exit(comum.ExitTimeout)
	case <-sig:
		os.Exit(comum.ExitCanceled)
	}
}

func exitErr(err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		os.Exit(comum.ExitTimeout)
	}
	if errors.Is(err, context.Canceled) {
		os.Exit(comum.ExitCanceled)
	}
	fmt.Fprintf(os.Stderr, "waitfor: %v\n", err)
	os.Exit(1)
}
