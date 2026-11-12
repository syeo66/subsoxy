package logsafe

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func FuzzSanitize(f *testing.F) {
	f.Add("plain", 10)
	f.Add("line\nbreak\r\x1b[31mred\x7f", 100)
	f.Add(strings.Repeat("é", 10), 5)
	f.Add(strings.Repeat("😀", 10), 7)
	f.Add("\xff\xfe invalid", 3)
	f.Add("", 0)

	f.Fuzz(func(t *testing.T, input string, maxLength int) {
		if maxLength < 0 || maxLength > 2*MaxLength {
			return
		}
		got := sanitize(input, maxLength)

		for i := 0; i < len(got); i++ {
			if got[i] < ASCIIControlCharMin || got[i] == ASCIIControlCharMax {
				t.Fatalf("sanitize(%q, %d) = %q keeps control byte %#x", input, maxLength, got, got[i])
			}
		}
		if !utf8.ValidString(got) {
			t.Fatalf("sanitize(%q, %d) = %q isn't valid UTF-8", input, maxLength, got)
		}

		// Without the cut, the result is the input minus its control characters, with
		// invalid bytes replaced
		filtered := strings.Map(func(r rune) rune {
			if r < ASCIIControlCharMin || r == ASCIIControlCharMax {
				return -1
			}
			return r
		}, input)
		if len(filtered) <= maxLength {
			if got != filtered {
				t.Fatalf("sanitize(%q, %d) = %q, want it uncut as %q", input, maxLength, got, filtered)
			}
			return
		}

		// A cut result is a prefix of whole characters that fits, followed by "..."
		kept, ok := strings.CutSuffix(got, "...")
		if !ok {
			t.Fatalf("sanitize(%q, %d) = %q is cut without \"...\"", input, maxLength, got)
		}
		if len(kept) > maxLength {
			t.Fatalf("sanitize(%q, %d) = %q keeps %d bytes", input, maxLength, got, len(kept))
		}
		if !strings.HasPrefix(filtered, kept) {
			t.Fatalf("sanitize(%q, %d) = %q isn't a prefix of %q", input, maxLength, got, filtered)
		}
		// Only the character the limit falls in is dropped
		if _, size := utf8.DecodeRuneInString(filtered[len(kept):]); len(kept)+size <= maxLength {
			t.Fatalf("sanitize(%q, %d) = %q drops more than needed", input, maxLength, got)
		}
	})
}
