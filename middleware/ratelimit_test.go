package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"testing"
	"time"
)

func newRequest(remoteAddr string, forwardedFor ...string) *http.Request {
	r := httptest.NewRequest("GET", "/rest/ping", nil)
	r.RemoteAddr = remoteAddr
	for _, value := range forwardedFor {
		r.Header.Add("X-Forwarded-For", value)
	}
	return r
}

func mustPrefixes(t *testing.T, prefixes ...string) []netip.Prefix {
	t.Helper()
	var result []netip.Prefix
	for _, p := range prefixes {
		result = append(result, netip.MustParsePrefix(p))
	}
	return result
}

func TestClientIP(t *testing.T) {
	trusted := mustPrefixes(t, "10.0.0.0/8", "fd00::/8")

	tests := []struct {
		name         string
		remoteAddr   string
		forwardedFor []string
		trusted      []netip.Prefix
		want         string
	}{
		{"direct client", "203.0.113.7:51234", nil, trusted, "203.0.113.7"},
		{"forwarded header from untrusted peer is ignored", "203.0.113.7:51234", []string{"198.51.100.1"}, trusted, "203.0.113.7"},
		{"no trusted proxies configured", "10.0.0.2:51234", []string{"198.51.100.1"}, nil, "10.0.0.2"},
		{"trusted proxy", "10.0.0.2:51234", []string{"198.51.100.1"}, trusted, "198.51.100.1"},
		{"spoofed entries left of the client are ignored", "10.0.0.2:51234", []string{"1.1.1.1, 198.51.100.1"}, trusted, "198.51.100.1"},
		{"chain of trusted proxies", "10.0.0.2:51234", []string{"198.51.100.1, 10.0.0.9"}, trusted, "198.51.100.1"},
		{"several header lines", "10.0.0.2:51234", []string{"1.1.1.1", "198.51.100.1"}, trusted, "198.51.100.1"},
		{"trusted proxy without header", "10.0.0.2:51234", nil, trusted, "10.0.0.2"},
		{"malformed entry stops at last good hop", "10.0.0.2:51234", []string{"198.51.100.1, garbage"}, trusted, "10.0.0.2"},
		{"entry with port", "10.0.0.2:51234", []string{"198.51.100.1:4444"}, trusted, "198.51.100.1"},
		{"IPv4-mapped IPv6 peer", "[::ffff:10.0.0.2]:51234", []string{"198.51.100.1"}, trusted, "198.51.100.1"},
		{"IPv6 proxy", "[fd00::1]:51234", []string{"2001:db8::5"}, trusted, "2001:db8::5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClientIP(newRequest(tt.remoteAddr, tt.forwardedFor...), tt.trusted)
			if got.String() != tt.want {
				t.Errorf("ClientIP() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestClientRateLimiterLimitsEachClientSeparately(t *testing.T) {
	limiter := NewClientRateLimiter(1, 2, nil)

	for i := 0; i < 2; i++ {
		if _, ok := limiter.Allow(newRequest("203.0.113.1:1000")); !ok {
			t.Fatalf("Request %d from first client should be allowed", i+1)
		}
	}
	if _, ok := limiter.Allow(newRequest("203.0.113.1:1001")); ok {
		t.Error("First client should be limited after its burst")
	}
	if _, ok := limiter.Allow(newRequest("203.0.113.2:1000")); !ok {
		t.Error("Second client should not be affected by the first client's limit")
	}
}

func TestClientRateLimiterUsesForwardedClientBehindTrustedProxy(t *testing.T) {
	limiter := NewClientRateLimiter(1, 1, mustPrefixes(t, "10.0.0.0/8"))

	if _, ok := limiter.Allow(newRequest("10.0.0.2:1000", "198.51.100.1")); !ok {
		t.Fatal("First client's request should be allowed")
	}
	if _, ok := limiter.Allow(newRequest("10.0.0.2:1000", "198.51.100.2")); !ok {
		t.Error("Another client behind the same proxy should have its own bucket")
	}
	if _, ok := limiter.Allow(newRequest("10.0.0.2:1000", "198.51.100.1")); ok {
		t.Error("First client should be limited after its burst")
	}
}

func TestClientRateLimiterGroupsIPv6By64(t *testing.T) {
	limiter := NewClientRateLimiter(1, 1, nil)

	key, ok := limiter.Allow(newRequest("[2001:db8:1:2::1]:1000"))
	if !ok {
		t.Fatal("First request should be allowed")
	}
	if key != "2001:db8:1:2::/64" {
		t.Errorf("Expected IPv6 client keyed by its /64, got %s", key)
	}
	if _, ok := limiter.Allow(newRequest("[2001:db8:1:2::99]:1000")); ok {
		t.Error("Another address in the same /64 should share the bucket")
	}
	if _, ok := limiter.Allow(newRequest("[2001:db8:1:3::1]:1000")); !ok {
		t.Error("An address in a different /64 should have its own bucket")
	}
}

func TestClientRateLimiterSharesOverflowBucketWhenFull(t *testing.T) {
	limiter := NewClientRateLimiter(1, 1, nil)
	for i := 0; i < MaxTrackedClients; i++ {
		limiter.clients["client"+strconv.Itoa(i)] = &clientBucket{limiter: nil, lastSeen: time.Now()}
	}

	key, ok := limiter.Allow(newRequest("203.0.113.1:1000"))
	if key != overflowClientKey || !ok {
		t.Fatalf("Expected first new client to use the overflow bucket, got key=%s allowed=%v", key, ok)
	}
	if _, ok := limiter.Allow(newRequest("203.0.113.2:1000")); ok {
		t.Error("New clients beyond the limit should share the overflow bucket")
	}
	if got := len(limiter.clients); got != MaxTrackedClients+1 {
		t.Errorf("Expected %d tracked buckets, got %d", MaxTrackedClients+1, got)
	}
}

func TestClientRateLimiterDropsIdleClients(t *testing.T) {
	limiter := NewClientRateLimiter(1, 1, nil)
	limiter.Allow(newRequest("203.0.113.1:1000"))

	limiter.clients["203.0.113.1"].lastSeen = time.Now().Add(-ClientIdleTimeout)
	limiter.lastSweep = time.Now().Add(-ClientSweepInterval)
	limiter.Allow(newRequest("203.0.113.2:1000"))

	if _, exists := limiter.clients["203.0.113.1"]; exists {
		t.Error("Idle client's bucket should have been dropped")
	}
	if _, exists := limiter.clients["203.0.113.2"]; !exists {
		t.Error("Active client's bucket should be kept")
	}
}
