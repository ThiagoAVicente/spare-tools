package comum

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		err  bool
	}{
		{"25m", 25 * time.Minute, false},
		{"1h30m", 90 * time.Minute, false},
		{"90", 90 * time.Second, false},
		{"1:30:00", 90 * time.Minute, false},
		{"1:30", 90 * time.Second, false},
		{"5s", 5 * time.Second, false},
		{"1d", 24 * time.Hour, false},
		{"2h", 2 * time.Hour, false},
		{"0", 0, false},
		{"1.5h", 90 * time.Minute, false},
		{"abc", 0, true},
		{"", 0, true},
		{"-5", 0, true},
		{"5x", 0, true},
		{"m", 0, true},
		{"1:2:3:4", 0, true},
	}
	for _, c := range cases {
		got, err := ParseDuration(c.in)
		if c.err {
			if err == nil {
				t.Errorf("ParseDuration(%q): expected error, got %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDuration(%q): unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseDuration(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{2*time.Minute + 14*time.Second, "2m14s"},
		{90 * time.Minute, "1h30m"},
		{45 * time.Second, "45s"},
		{time.Hour, "1h"},
		{0, "0s"},
		{time.Hour + 5*time.Second, "1h0m5s"},
	}
	for _, c := range cases {
		if got := FormatDuration(c.in); got != c.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
