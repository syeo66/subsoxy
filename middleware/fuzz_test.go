package middleware

import (
	"encoding/binary"
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"github.com/syeo66/subsoxy/config"
)

// FuzzClientIP checks that X-Forwarded-For is only believed through a chain of trusted
// proxies: the client address is never taken from left of a hop that isn't trusted.
func FuzzClientIP(f *testing.F) {
	f.Add("10.0.0.1:1234", "203.0.113.5", "", "10.0.0.0/8")
	f.Add("10.0.0.1:1234", "1.1.1.1, 203.0.113.5, 10.0.0.2", "", "10.0.0.0/8")
	f.Add("10.0.0.1:1234", "203.0.113.5", "10.0.0.3", "10.0.0.0/8")
	f.Add("203.0.113.9:80", "10.0.0.5", "", "10.0.0.0/8")
	f.Add("[::ffff:10.0.0.1]:443", "garbage, 2001:db8::1", "", "10.0.0.1")
	f.Add("[fe80::1%eth0]:80", "fe80::2%lo", "", "fe80::/10")
	f.Add("unix", "1.2.3.4", "", "")
	f.Add("10.0.0.3", "0, 10.0.0.3", "", "0.0.0.0/0")

	f.Fuzz(func(t *testing.T, remoteAddr, forwarded1, forwarded2, trusted string) {
		prefixes, err := config.ParseTrustedProxies(trusted)
		if err != nil {
			return
		}
		r := &http.Request{RemoteAddr: remoteAddr, Header: http.Header{}}
		var hops []string
		for _, value := range []string{forwarded1, forwarded2} {
			if value != "" {
				r.Header.Add("X-Forwarded-For", value)
				hops = append(hops, strings.Split(value, ",")...)
			}
		}

		got := ClientIP(r, prefixes)

		if got.Is4In6() || got.Zone() != "" {
			t.Fatalf("ClientIP = %v, want it without zone or IPv4 mapping", got)
		}
		remote := parseIP(remoteAddr)
		if !remote.IsValid() || !isTrusted(remote, prefixes) {
			if got != remote {
				t.Fatalf("connection from untrusted %v, but ClientIP = %v", remote, got)
			}
			return
		}

		// hops[reach:] is the run of trusted proxies the request passed through last
		reach := len(hops)
		for reach > 0 {
			hop := parseIP(strings.TrimSpace(hops[reach-1]))
			if !hop.IsValid() || !isTrusted(hop, prefixes) {
				break
			}
			reach--
		}
		// The client is the hop that handed the request to the first of those proxies. When
		// there's no valid one, the farthest trusted proxy is as close as it gets.
		want := remote
		if reach > 0 && parseIP(strings.TrimSpace(hops[reach-1])).IsValid() {
			want = parseIP(strings.TrimSpace(hops[reach-1]))
		} else if reach < len(hops) {
			want = parseIP(strings.TrimSpace(hops[reach]))
		}
		if got != want {
			t.Fatalf("ClientIP = %v, want %v for remote %v, hops %q, trusted %v", got, want, remote, hops, prefixes)
		}
	})
}

// FuzzClientKey checks that all IPv6 addresses in a /64 share one rate limit bucket, and
// that IPv4 addresses each get their own.
func FuzzClientKey(f *testing.F) {
	f.Add([]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1}, uint64(0xffff))
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 10, 0, 0, 1}, uint64(1))
	f.Add([]byte{192, 0, 2, 1}, uint64(0))

	f.Fuzz(func(t *testing.T, raw []byte, host uint64) {
		addr, ok := netip.AddrFromSlice(raw)
		if !ok {
			if clientKey(netip.Addr{}) != "unknown" {
				t.Fatal("an invalid address must count against the unknown bucket")
			}
			return
		}

		key := clientKey(addr)
		if addr.Is4() {
			if key != addr.String() {
				t.Fatalf("clientKey(%v) = %q, want the address itself", addr, key)
			}
			return
		}

		// Another address in the same /64
		bytes := addr.As16()
		binary.BigEndian.PutUint64(bytes[8:], host)
		neighbour := netip.AddrFrom16(bytes)
		if other := clientKey(neighbour); other != key {
			t.Fatalf("clientKey(%v) = %q but clientKey(%v) = %q, want one bucket per /64", addr, key, neighbour, other)
		}

		// An address in a different /64
		binary.BigEndian.PutUint64(bytes[:8], binary.BigEndian.Uint64(bytes[:8])^1)
		if other := clientKey(netip.AddrFrom16(bytes)); other == key {
			t.Fatalf("clientKey(%v) and clientKey(%v) are both %q, want separate buckets", addr, netip.AddrFrom16(bytes), key)
		}
	})
}
