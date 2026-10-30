// Package logsafe prepares user-supplied values for logging
package logsafe

import (
	"strings"
	"unicode/utf8"
)

const (
	MaxLength         = 1000
	MaxUsernameLength = 100
)

// ASCII control character constants
const (
	ASCIIControlCharMin = 32
	ASCIIControlCharMax = 127
)

// String removes control characters and limits length to prevent log injection
func String(input string) string {
	return sanitize(input, MaxLength)
}

// Username sanitizes a username for logging
func Username(username string) string {
	return sanitize(username, MaxUsernameLength)
}

func sanitize(input string, maxLength int) string {
	// Remove control characters (ASCII 0-31 and 127)
	sanitized := strings.Map(func(r rune) rune {
		if r < ASCIIControlCharMin || r == ASCIIControlCharMax {
			return -1
		}
		return r
	}, input)

	// Limit length to prevent resource exhaustion, without cutting a character in half
	if len(sanitized) > maxLength {
		cut := maxLength
		for cut > 0 && !utf8.RuneStart(sanitized[cut]) {
			cut--
		}
		sanitized = sanitized[:cut] + "..."
	}

	return sanitized
}
