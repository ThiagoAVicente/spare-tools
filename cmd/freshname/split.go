package main

import "strings"

// compoundExts are extensions treated as a single unit rather than
// splitting off only the final ".ext".
var compoundExts = []string{".tar.gz", ".tar.xz", ".tar.bz2", ".tar.zst"}

// splitExt splits a basename into (base, ext) following these rules:
//   - Compound extensions (.tar.gz, .tar.xz, .tar.bz2, .tar.zst) are kept
//     whole as one unit.
//   - Extensionless names (or directories) return the whole name as base
//     with an empty ext.
//   - A leading dot does not count as an extension start (hidden files):
//     ".hidden" -> (".hidden", ""), ".hidden.txt" -> (".hidden", ".txt").
//   - Otherwise, the base is everything before the last dot, and ext is
//     the last dot onward.
func splitExt(name string) (base string, ext string) {
	for _, ce := range compoundExts {
		if strings.HasSuffix(name, ce) && len(name) > len(ce) {
			return name[:len(name)-len(ce)], ce
		}
	}

	// Find the last dot, but ignore a dot at position 0 (hidden file
	// marker) when determining the split point.
	searchFrom := 0
	if strings.HasPrefix(name, ".") {
		searchFrom = 1
	}

	idx := strings.LastIndex(name[searchFrom:], ".")
	if idx < 0 {
		return name, ""
	}
	idx += searchFrom

	return name[:idx], name[idx:]
}
