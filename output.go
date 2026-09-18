package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode"
	"unicode/utf8"
)

// printJSON pretty-prints any value as JSON to stdout. An encode/write
// failure (broken pipe aside) must not pass silently — flagged by GoLand:
// every call site was discarding the error.
func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fatal("writing JSON output: %v", err)
	}
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

// unsafeRune reports whether r must not reach a terminal verbatim: C0/C1
// controls and DEL (ESC, CR, LF, TAB, …), format characters (category Cf:
// bidi overrides and isolates, zero-width characters, BOM, tags), and the
// line/paragraph separators.
func unsafeRune(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == 0x2029
}

// sanitize makes remote text safe to print for a human: every unsafeRune
// becomes its visible Go escape (\x1b, \n, \u202e) and every invalid UTF-8
// byte becomes \xNN, so tampering shows instead of acting on the terminal.
// All other text is returned byte-for-byte. JSON and CSV output do their
// own escaping and must not go through here.
func sanitize(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1: // invalid byte, not a real U+FFFD
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case unsafeRune(r):
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// row sanitizes each cell before joining: the only raw tabs and newlines
// the tabwriter sees are this function's own separators, so a cell can
// neither forge a row nor (via 0xff, tabwriter's escape marker) swallow one.
func (t *table) row(cells ...string) {
	safe := make([]string, len(cells))
	for i, c := range cells {
		safe[i] = sanitize(c)
	}
	fmt.Fprintln(t.w, strings.Join(safe, "\t"))
}

func (t *table) flush() {
	t.w.Flush()
}

// truncate caps s at n bytes. The cut backs up to a rune boundary: half a
// rune is invalid UTF-8, which sanitize would then print as \xNN noise in
// an otherwise legitimate name.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut, tail := n-1, "…"
	if n <= 1 {
		cut, tail = n, ""
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + tail
}

// fatal sanitizes the finished message, not the format: errors such as
// GraphQLError carry server-supplied text.
func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: %s\n", sanitize(fmt.Sprintf(format, args...)))
	os.Exit(1)
}
