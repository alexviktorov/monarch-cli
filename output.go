package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"text/tabwriter"
)

// printJSON pretty-prints any value as JSON to stdout.
func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// money formats a float as a dollar amount with thousands separators,
// e.g. 1234567.891 -> "$1,234,567.89", -42.5 -> "-$42.50".
func money(v float64) string {
	neg := v < 0
	abs := math.Abs(v)
	whole := int64(abs)
	cents := int64(math.Round((abs - float64(whole)) * 100))
	if cents == 100 { // rounding pushed us to the next whole unit
		whole++
		cents = 0
	}
	s := fmt.Sprintf("%d", whole)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	out := fmt.Sprintf("$%s.%02d", b.String(), cents)
	if neg {
		out = "-" + out
	}
	return out
}

// table is a tiny helper around tabwriter.
type table struct {
	w *tabwriter.Writer
}

func newTable(out io.Writer, headers ...string) *table {
	t := &table{w: tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)}
	fmt.Fprintln(t.w, strings.Join(headers, "\t"))
	sep := make([]string, len(headers))
	for i, h := range headers {
		sep[i] = strings.Repeat("-", len(h))
	}
	fmt.Fprintln(t.w, strings.Join(sep, "\t"))
	return t
}

func (t *table) row(cells ...string) {
	fmt.Fprintln(t.w, strings.Join(cells, "\t"))
}

func (t *table) flush() {
	t.w.Flush()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
