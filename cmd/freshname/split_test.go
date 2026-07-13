package main

import "testing"

func TestSplitExt(t *testing.T) {
	cases := []struct {
		name     string
		wantBase string
		wantExt  string
	}{
		{"a.txt", "a", ".txt"},
		{"b.tar.gz", "b", ".tar.gz"},
		{"noext", "noext", ""},
		{".hidden", ".hidden", ""},
		{".hidden.txt", ".hidden", ".txt"},
		{"x.tar.bz2", "x", ".tar.bz2"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			base, ext := splitExt(c.name)
			if base != c.wantBase || ext != c.wantExt {
				t.Errorf("splitExt(%q) = (%q, %q), want (%q, %q)", c.name, base, ext, c.wantBase, c.wantExt)
			}
		})
	}
}
