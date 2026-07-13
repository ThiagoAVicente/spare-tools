// Command freshname prints a name derived from PATH that is guaranteed
// not to already exist, inserting an incrementing numeric suffix before
// the extension when PATH is already taken.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

const helpText = `Usage: freshname [--create] [--sep S] [--start N] PATH

Print a name derived from PATH that does not already exist.

If PATH does not exist, it is printed unchanged. Otherwise, freshname
tries "name<sep>N.ext", "name<sep>N+1.ext", ... (starting at N=--start)
until it finds one that is free.

Flags:
  --create     Atomically create the resulting file (via O_CREATE|O_EXCL)
               instead of merely checking whether it exists, and print
               the name that was actually created.
  --sep S      Separator string inserted before the numeric suffix
               (default "-").
  --start N    First suffix number to try (default 2).

Concurrency note:
  Without --create, freshname only stat()s candidate names and is
  inherently racy (check-then-print, no locking): two concurrent
  invocations may both report the same "fresh" name. For scripts that
  run concurrently, use --create, which atomically creates the file
  with O_EXCL and is safe under concurrent use.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("freshname", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, helpText)
	}

	create := fs.Bool("create", false, "atomically create the resulting file")
	sep := fs.String("sep", "-", "separator string before the numeric suffix")
	start := fs.Int("start", 2, "first suffix number to try")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	path := fs.Arg(0)

	if *create {
		name, err := createFresh(path, *sep, *start)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, name)
		return 0
	}

	name := findFresh(path, *sep, *start)
	fmt.Fprintln(stdout, name)
	return 0
}

// findFresh returns the first candidate derived from path that does not
// exist on disk. This is inherently racy (stat-then-use); see --create
// for an atomic alternative.
func findFresh(path string, sep string, start int) string {
	if !exists(path) {
		return path
	}

	dir := filepath.Dir(path)
	base, ext := splitExt(filepath.Base(path))

	for n := start; ; n++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s%s%d%s", base, sep, n, ext))
		if !exists(candidate) {
			return candidate
		}
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// createFresh atomically creates the first candidate derived from path
// that does not already exist, using O_CREATE|O_EXCL to avoid TOCTOU
// races, and returns the name that was created.
func createFresh(path string, sep string, start int) (string, error) {
	dir := filepath.Dir(path)
	base, ext := splitExt(filepath.Base(path))

	candidate := path
	n := start
	for {
		f, err := os.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			f.Close()
			return candidate, nil
		}
		if os.IsExist(err) {
			candidate = filepath.Join(dir, fmt.Sprintf("%s%s%d%s", base, sep, n, ext))
			n++
			continue
		}
		return "", err
	}
}
