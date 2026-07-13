// Package comum holds the shared bits every spare-tool needs:
// duration parsing, TTY detection, and the exit-code convention.
package comum

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

// Exit-code convention, respected by every tool in the workspace.
const (
	ExitOK       = 0   // success
	ExitTempFail = 75  // EX_TEMPFAIL: lock busy (alone)
	ExitTimeout  = 124 // timeout(1) convention (waitfor --timeout)
	ExitCanceled = 130 // 128+SIGINT: canceled by user
)

// ParseDuration accepts "25m", "1h30m", "90" (bare seconds) and "1:30:00"
// (h:mm:ss or m:ss) and returns the duration.
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}

	// Clock form: "1:30:00" (h:mm:ss) or "1:30" (m:ss).
	if strings.Contains(s, ":") {
		return parseClock(s)
	}

	// Bare number: seconds.
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		if n < 0 {
			return 0, fmt.Errorf("negative duration %q", s)
		}
		return time.Duration(n * float64(time.Second)), nil
	}

	// Composed units: "1h30m", "25m", "90s", "1d2h".
	return parseUnits(s)
}

func parseClock(s string) (time.Duration, error) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("invalid clock duration %q", s)
	}
	var nums []int
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid clock duration %q", s)
		}
		nums = append(nums, n)
	}
	var d time.Duration
	if len(nums) == 3 {
		d = time.Duration(nums[0])*time.Hour + time.Duration(nums[1])*time.Minute + time.Duration(nums[2])*time.Second
	} else {
		d = time.Duration(nums[0])*time.Minute + time.Duration(nums[1])*time.Second
	}
	return d, nil
}

var unitMap = map[string]time.Duration{
	"s": time.Second,
	"m": time.Minute,
	"h": time.Hour,
	"d": 24 * time.Hour,
}

func parseUnits(s string) (time.Duration, error) {
	var total time.Duration
	num := ""
	seen := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9' || r == '.':
			num += string(r)
		default:
			unit, ok := unitMap[strings.ToLower(string(r))]
			if !ok || num == "" {
				return 0, fmt.Errorf("invalid duration %q", s)
			}
			n, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid duration %q", s)
			}
			total += time.Duration(n * float64(unit))
			num = ""
			seen = true
		}
	}
	if num != "" || !seen {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	return total, nil
}

// FormatDuration renders a duration compactly: "2m14s", "1h30m", "45s".
func FormatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	switch {
	case h > 0 && (m > 0 || s > 0):
		if s > 0 {
			return fmt.Sprintf("%dh%dm%ds", h, m, s)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	case m > 0 && s > 0:
		return fmt.Sprintf("%dm%ds", m, s)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// StdoutIsTTY reports whether stdout is a terminal. Tools use this to
// switch between human output (colors, progress) and raw pipe output.
func StdoutIsTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}
