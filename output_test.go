package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"testing"
	"unicode/utf8"
)

// Hostile runes are spelled by code point so this source never carries a raw
// control or bidi character itself.
var (
	c1CSI            = string(rune(0x9b))    // C1 control sequence introducer
	bidiOverride     = string(rune(0x202e))  // RIGHT-TO-LEFT OVERRIDE
	bidiIsolate      = string(rune(0x2066))  // LEFT-TO-RIGHT ISOLATE
	popIsolate       = string(rune(0x2069))  // POP DIRECTIONAL ISOLATE
	zeroWidthSpace   = string(rune(0x200b))  // ZERO WIDTH SPACE
	zeroWidthJoiner  = string(rune(0x200d))  // ZERO WIDTH JOINER
	byteOrderMark    = string(rune(0xfeff))  // ZERO WIDTH NO-BREAK SPACE
	lineSeparator    = string(rune(0x2028))  // LINE SEPARATOR
	paraSeparator    = string(rune(0x2029))  // PARAGRAPH SEPARATOR
	tagCharacter     = string(rune(0xe0001)) // LANGUAGE TAG
	replacementGlyph = string(rune(0xfffd))  // a genuine U+FFFD, not a decode error
)

func TestSanitize(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		// Ordinary text must come back byte-for-byte.
		{"empty", "", ""},
		{"plain ASCII", "Whole Foods #123", "Whole Foods #123"},
		{"backslashes and quotes stay literal", `C:\tmp "x" 'y'`, `C:\tmp "x" 'y'`},
		{"non-ASCII text, emoji and ellipsis", "Café Zoë — 東京 🍕…", "Café Zoë — 東京 🍕…"},
		{"genuine replacement character", "a" + replacementGlyph + "b", "a" + replacementGlyph + "b"},

		// Terminal control: C0, DEL, C1.
		{"CSI clear screen and home", "\x1b[2J\x1b[H", `\x1b[2J\x1b[H`},
		{"OSC 52 clipboard write", "\x1b]52;c;QUFBQQ==\a", `\x1b]52;c;QUFBQQ==\a`},
		{"CR LF TAB", "a\r\nb\tc", `a\r\nb\tc`},
		{"BS DEL NUL", "\b\x7f\x00", `\b\x7f\x00`},
		{"VT FF", "\v\f", `\v\f`},
		{"C1 CSI", c1CSI + "31m", `\u009b31m`},

		// Unicode format characters and separators.
		{"bidi override", "abc" + bidiOverride + "def", `abc\u202edef`},
		{"bidi isolates", bidiIsolate + "x" + popIsolate, `\u2066x\u2069`},
		{"zero width space and joiner", "a" + zeroWidthSpace + "b" + zeroWidthJoiner + "c", `a\u200bb\u200dc`},
		{"byte order mark", byteOrderMark + "x", `\ufeffx`},
		{"line and paragraph separators", "a" + lineSeparator + "b" + paraSeparator, `a\u2028b\u2029`},
		{"tag character", tagCharacter, `\U000e0001`},

		// Invalid UTF-8: 0xff is also text/tabwriter's escape marker.
		{"invalid byte", "a\xffb", `a\xffb`},
		{"truncated lead byte", "caf\xc3", `caf\xc3`},
		{"stray continuation byte", "\x80", `\x80`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitize(tt.in)
			if got != tt.want {
				t.Errorf("sanitize(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if again := sanitize(got); again != got {
				t.Errorf("not idempotent: sanitize(%q) = %q", got, again)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in   string
		n    int
		want string
	}{
		// ASCII contract.
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hell…"},
		{"hello", 1, "h"},
		{"hello", 0, ""},

		// The budget is bytes, so these cut where they always did.
		{"éééééé", 5, "éé…"},
		{"日本語テキスト", 4, "日…"},
		{"🍕🍕🍕", 5, "🍕…"},
		{"aé", 1, "a"},
		{"Café Zoë Brasserie & Pâtisserie", 30, "Café Zoë Brasserie & Pâtis…"},

		// A cut landing inside a rune backs up to the rune's start instead
		// of emitting half of it.
		{"日本語テキスト", 5, "日…"},
		{"日本語テキスト", 6, "日…"},
		{"🍕🍕🍕", 2, "…"},
		{"éa", 1, ""},
	}
	for _, tt := range tests {
		if got := truncate(tt.in, tt.n); got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
		}
	}
}

func TestTruncateNeverSplitsARune(t *testing.T) {
	for _, s := range []string{"Café Zoë", "日本語テキスト", "Pizza 🍕🍕🍕 party", "naïve—dash…"} {
		for n := 0; n <= len(s)+1; n++ {
			if got := truncate(s, n); !utf8.ValidString(got) {
				t.Errorf("truncate(%q, %d) = %q: invalid UTF-8", s, n, got)
			}
		}
	}
}

func TestTableRowSanitizesCells(t *testing.T) {
	tests := []struct {
		name string
		cell string
		want string
	}{
		// A newline plus tabs in one cell must not become a second row.
		{"forged row", "x\ny\tz",
			"A        B\n" +
				"-        -\n" +
				`x\ny\tz  1` + "\n" +
				"ok       2\n"},
		// A raw 0xff would make tabwriter swallow the separators after it.
		{"tabwriter escape byte", "a\xffb",
			"A       B\n" +
				"-       -\n" +
				`a\xffb  1` + "\n" +
				"ok      2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tb := newTable(&buf, "A", "B")
			tb.row(tt.cell, "1")
			tb.row("ok", "2")
			tb.flush()
			if got := buf.String(); got != tt.want {
				t.Errorf("table output mismatch\n--- got ---\n%q\n--- want ---\n%q", got, tt.want)
			}
		})
	}
}

// fatal exits the process, so the test re-runs its own binary as the child
// and inspects what that child wrote to stderr.
func TestFatalSanitizesMessage(t *testing.T) {
	if os.Getenv("MONARCH_TEST_FATAL_CHILD") == "1" {
		// Server text reaches fatal through GraphQLError.Error().
		fatal("login failed: %v", errors.New("denied\x1b[2J\nerror: forged line"))
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestFatalSanitizesMessage$")
	cmd.Env = append(os.Environ(), "MONARCH_TEST_FATAL_CHILD=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("child exit = %v, want exit status 1", err)
	}
	want := `error: login failed: denied\x1b[2J\nerror: forged line` + "\n"
	if got := stderr.String(); got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}
