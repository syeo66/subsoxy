package config

import (
	"net/netip"
	"strings"
	"testing"
)

// FuzzParseTrustedProxies checks that every entry becomes one masked prefix, and that each
// prefix matches addresses in the form the rate limiter compares them: without zone, and
// with IPv4-mapped IPv6 addresses as plain IPv4.
func FuzzParseTrustedProxies(f *testing.F) {
	f.Add("10.0.0.1")
	f.Add("10.0.0.0/8, 192.168.1.7/24 ,2001:db8::/32")
	f.Add("::ffff:10.0.0.1")
	f.Add("::ffff:10.0.0.0/104")
	f.Add("fe80::1%eth0")
	f.Add(",,, ")
	f.Add("10.0.0.0/33")

	f.Fuzz(func(t *testing.T, input string) {
		prefixes, err := ParseTrustedProxies(input)
		if err != nil {
			return
		}

		var entries []string
		for entry := range strings.SplitSeq(input, ",") {
			if entry = strings.TrimSpace(entry); entry != "" {
				entries = append(entries, entry)
			}
		}
		if len(prefixes) != len(entries) {
			t.Fatalf("ParseTrustedProxies(%q) gave %d prefixes for %d entries", input, len(prefixes), len(entries))
		}

		for i, prefix := range prefixes {
			if !prefix.IsValid() || prefix != prefix.Masked() {
				t.Fatalf("entry %q became %v, want a valid masked prefix", entries[i], prefix)
			}
			// The rate limiter compares addresses unmapped and without zone
			network := prefix.Addr().Unmap().WithZone("")
			if !prefix.Contains(network) {
				t.Fatalf("entry %q became %v, which can never match the address %v the rate limiter sees", entries[i], prefix, network)
			}
			if addr, err := netip.ParseAddr(entries[i]); err == nil {
				if normalized := addr.Unmap().WithZone(""); !prefix.Contains(normalized) {
					t.Fatalf("entry %q became %v, which doesn't hold %v", entries[i], prefix, normalized)
				}
			}
		}
	})
}
