package logsafe

import (
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Normal text",
			input:    "normal text",
			expected: "normal text",
		},
		{
			name:     "All control characters",
			input:    "\x00\x01\x02\x1F\x7F",
			expected: "",
		},
		{
			name:     "Mixed control and normal",
			input:    "hello\x00world\x01test",
			expected: "helloworldtest",
		},
		{
			name:     "Newline and tab",
			input:    "line1\nline2\ttest",
			expected: "line1line2test",
		},
		{
			name:     "Very long string with control chars",
			input:    strings.Repeat("a", 500) + "\x01\x02" + strings.Repeat("b", 500),
			expected: strings.Repeat("a", 500) + strings.Repeat("b", 500),
		},
		{
			name:     "Unicode characters (should be preserved)",
			input:    "测试文本🎵",
			expected: "测试文本🎵",
		},
		{
			name:     "Only whitespace (should be preserved)",
			input:    "   \t   ",
			expected: "      ", // tabs converted to spaces, regular spaces preserved
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := String(tt.input)
			if result != tt.expected {
				t.Errorf("String(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestTruncation(t *testing.T) {
	if got := String(strings.Repeat("a", MaxLength+1)); got != strings.Repeat("a", MaxLength)+"..." {
		t.Errorf("Expected truncation to %d bytes plus ellipsis, got %d bytes", MaxLength, len(got))
	}
	// "é" is two bytes, so the limit falls inside one: it's dropped rather than split
	got := String(strings.Repeat("a", MaxLength-1) + "é")
	if got != strings.Repeat("a", MaxLength-1)+"..." {
		t.Errorf("Expected the split character to be dropped, got %q", got[len(got)-5:])
	}
	if got := Username(strings.Repeat("u", MaxUsernameLength+5)); len(got) != MaxUsernameLength+3 {
		t.Errorf("Expected username truncated to %d bytes plus ellipsis, got %d bytes", MaxUsernameLength, len(got))
	}
}
