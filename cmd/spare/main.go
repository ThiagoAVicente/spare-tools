// spare prints info about the spare-tools, fetched straight from GitHub.
// Each tool describes itself in cmd/<tool>/info.txt; spare reads those
// files from raw.githubusercontent.com, so it works without a local clone.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const usageText = `usage: spare [--repo OWNER/NAME] [--ref REF] [TOOL]

Prints the description of a spare-tool, fetched from GitHub — no local
clone needed. Without TOOL, lists every available tool.

  spare                    list all tools with a one-line summary
  spare waitfor            print waitfor's full description
  spare --ref v0.1 alone   read from a specific branch/tag

  --repo OWNER/NAME   repository to read from (default ThiagoAVicente/spare-tools)
  --ref REF           branch or tag (default main)

exit codes: 0 ok; 1 unknown tool or network error.
`

type client struct {
	repo    string
	ref     string
	rawBase string // https://raw.githubusercontent.com (overridable for tests)
	apiBase string // https://api.github.com (overridable for tests)
	http    *http.Client
}

func newClient(repo, ref string) *client {
	rawBase := os.Getenv("SPARE_RAW_BASE")
	if rawBase == "" {
		rawBase = "https://raw.githubusercontent.com"
	}
	apiBase := os.Getenv("SPARE_API_BASE")
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	return &client{
		repo:    repo,
		ref:     ref,
		rawBase: rawBase,
		apiBase: apiBase,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *client) infoURL(tool string) string {
	return fmt.Sprintf("%s/%s/%s/cmd/%s/info.txt", c.rawBase, c.repo, c.ref, tool)
}

func (c *client) listURL() string {
	return fmt.Sprintf("%s/repos/%s/contents/cmd?ref=%s", c.apiBase, c.repo, c.ref)
}

var errNotFound = errors.New("not found")

func (c *client) get(url string) ([]byte, error) {
	resp, err := c.http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// info fetches a tool's full info.txt.
func (c *client) info(tool string) (string, error) {
	b, err := c.get(c.infoURL(tool))
	if errors.Is(err, errNotFound) {
		return "", fmt.Errorf("unknown tool %q (no cmd/%s/info.txt in %s@%s)", tool, tool, c.repo, c.ref)
	}
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// tools lists the tool directories under cmd/ via the GitHub contents API.
func (c *client) tools() ([]string, error) {
	b, err := c.get(c.listURL())
	if err != nil {
		return nil, err
	}
	var entries []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(b, &entries); err != nil {
		return nil, fmt.Errorf("parsing GitHub API response: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.Type == "dir" {
			names = append(names, e.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// summary is a tool's first info.txt line, or "" when unavailable.
func (c *client) summary(tool string) string {
	text, err := c.info(tool)
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(text, "\n")
	return line
}

func run() int {
	fs := flag.NewFlagSet("spare", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usageText) }
	repo := fs.String("repo", "ThiagoAVicente/spare-tools", "")
	ref := fs.String("ref", "main", "")
	if err := fs.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 1 {
		fs.Usage()
		return 2
	}
	c := newClient(*repo, *ref)

	if fs.NArg() == 1 {
		text, err := c.info(fs.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, "spare:", err)
			return 1
		}
		fmt.Print(text)
		return 0
	}

	names, err := c.tools()
	if err != nil {
		fmt.Fprintln(os.Stderr, "spare:", err)
		return 1
	}
	summaries := make([]string, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			summaries[i] = c.summary(name)
		}()
	}
	wg.Wait()
	for i, name := range names {
		if s := summaries[i]; s != "" {
			fmt.Println(s)
		} else {
			fmt.Println(name)
		}
	}
	return 0
}

func main() {
	os.Exit(run())
}
