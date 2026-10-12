package middleware

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	// A client's bucket is dropped after this long without requests. By then it has refilled
	// completely, so a fresh bucket behaves the same.
	ClientIdleTimeout = 10 * time.Minute
	// How often idle buckets are looked for
	ClientSweepInterval = time.Minute
	// Upper bound on clients with their own bucket. Further clients share one overflow bucket
	// until idle ones are dropped, so a flood from many addresses can't grow memory unbounded.
	MaxTrackedClients = 10000
	// IPv6 clients are grouped by their /64, since a single host usually controls a whole one
	IPv6ClientPrefixBits = 64
	// Key of the bucket shared by clients beyond MaxTrackedClients
	overflowClientKey = "overflow"
)

type clientBucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// ClientRateLimiter gives every client its own token bucket, so one busy client can't use up
// the limit for everyone else. Clients are identified by IP address (see ClientIP).
type ClientRateLimiter struct {
	rps            rate.Limit
	burst          int
	trustedProxies []netip.Prefix

	mu        sync.Mutex
	clients   map[string]*clientBucket
	lastSweep time.Time
}

// NewClientRateLimiter allows each client rps requests per second with bursts of up to burst
func NewClientRateLimiter(rps, burst int, trustedProxies []netip.Prefix) *ClientRateLimiter {
	return &ClientRateLimiter{
		rps:            rate.Limit(rps),
		burst:          burst,
		trustedProxies: trustedProxies,
		clients:        make(map[string]*clientBucket),
		lastSweep:      time.Now(),
	}
}

// Allow reports whether the request's client is within its limit, and returns the client key
// it was counted against
func (l *ClientRateLimiter) Allow(r *http.Request) (string, bool) {
	key := clientKey(ClientIP(r, l.trustedProxies))
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastSweep) >= ClientSweepInterval {
		l.sweep(now)
	}

	bucket, ok := l.clients[key]
	if !ok {
		if len(l.clients) >= MaxTrackedClients {
			key = overflowClientKey
			bucket = l.clients[key]
		}
		if bucket == nil {
			bucket = &clientBucket{limiter: rate.NewLimiter(l.rps, l.burst)}
			l.clients[key] = bucket
		}
	}
	bucket.lastSeen = now

	return key, bucket.limiter.AllowN(now, 1)
}

// sweep drops the buckets of clients idle for ClientIdleTimeout. The caller holds l.mu.
func (l *ClientRateLimiter) sweep(now time.Time) {
	l.lastSweep = now
	for key, bucket := range l.clients {
		if now.Sub(bucket.lastSeen) >= ClientIdleTimeout {
			delete(l.clients, key)
		}
	}
}

// ClientIP returns the address of the client that sent the request. X-Forwarded-For is only
// believed when the connection comes from a trusted proxy: its entries are read from the
// right, each added by the hop before it, and the first address that isn't a trusted proxy
// is the client. Entries further left were supplied by the client and are ignored.
func ClientIP(r *http.Request, trustedProxies []netip.Prefix) netip.Addr {
	addr := parseIP(r.RemoteAddr)
	if !addr.IsValid() || !isTrusted(addr, trustedProxies) {
		return addr
	}

	var hops []string
	for _, header := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(header, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		hop := parseIP(strings.TrimSpace(hops[i]))
		if !hop.IsValid() {
			break
		}
		addr = hop
		if !isTrusted(hop, trustedProxies) {
			break
		}
	}
	return addr
}

// parseIP parses an address with or without a port. It returns the zero Addr when s isn't one.
func parseIP(s string) netip.Addr {
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap().WithZone("")
}

func isTrusted(addr netip.Addr, trustedProxies []netip.Prefix) bool {
	for _, prefix := range trustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// clientKey is the bucket a client address counts against
func clientKey(addr netip.Addr) string {
	if !addr.IsValid() {
		return "unknown"
	}
	if addr.Is6() {
		prefix, _ := addr.Prefix(IPv6ClientPrefixBits)
		return prefix.String()
	}
	return addr.String()
}
