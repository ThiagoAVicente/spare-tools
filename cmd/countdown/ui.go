package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// renderBar renders a fixed-width progress bar like "[████░░░░░░]".
// frac is clamped to [0, 1].
func renderBar(frac float64, width int) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	filled := int(math.Round(frac * float64(width)))
	var b strings.Builder
	b.WriteByte('[')
	b.WriteString(strings.Repeat("█", filled))
	b.WriteString(strings.Repeat("░", width-filled))
	b.WriteByte(']')
	return b.String()
}

// formatClock renders a duration as MM:SS, or H:MM:SS when >= 1 hour.
func formatClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Round(time.Second).Seconds())
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
