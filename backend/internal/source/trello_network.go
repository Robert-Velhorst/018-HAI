package source

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

var trelloTransportCache struct {
	sync.Mutex
	timeout   time.Duration
	transport *http.Transport
}

var trelloNonPublicIPv4Prefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

var trelloNonPublicIPv6Prefixes = []netip.Prefix{
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

// trelloHTTPTransport keeps local test endpoints on the shared transport but
// pins remote Trello connections to an address that passed a Trello-specific
// public-address check. Shared transport loopback access requires an exact
// host:port opt-in and is not the DNS policy for credentialed remote requests.
func trelloHTTPTransport(base *url.URL) *http.Transport {
	if base == nil || isTrelloLoopbackHost(base.Hostname()) {
		return sourceHTTPTransport()
	}

	timeout := sourceHTTPTimeout()
	trelloTransportCache.Lock()
	defer trelloTransportCache.Unlock()
	if trelloTransportCache.transport != nil && trelloTransportCache.timeout == timeout {
		return trelloTransportCache.transport
	}
	if trelloTransportCache.transport != nil {
		trelloTransportCache.transport.CloseIdleConnections()
	}

	transport := sourceHTTPTransport().Clone()
	transport.DialContext = trelloSafeDialContext(timeout)
	trelloTransportCache.timeout = timeout
	trelloTransportCache.transport = transport
	return transport
}

func isTrelloLoopbackHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func trelloSafeDialContext(timeout time.Duration) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid Trello network address")
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve Trello API host: %w", err)
		}
		ip, err := trelloPublicResolvedAddress(ips)
		if err != nil {
			return nil, err
		}
		dialer := &net.Dialer{Timeout: timeout}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}
}

func trelloPublicResolvedAddress(ips []net.IPAddr) (netip.Addr, error) {
	if len(ips) == 0 {
		return netip.Addr{}, fmt.Errorf("Trello API host resolved to no addresses")
	}
	var selected netip.Addr
	for _, candidate := range ips {
		ip, ok := netip.AddrFromSlice(candidate.IP)
		if !ok {
			return netip.Addr{}, fmt.Errorf("Trello API host resolved to an invalid address")
		}
		ip = ip.Unmap()
		if !trelloPublicAddress(ip) {
			return netip.Addr{}, fmt.Errorf("Trello API host resolved to a non-public address")
		}
		if !selected.IsValid() {
			selected = ip
		}
	}
	return selected, nil
}

func trelloPublicAddress(ip netip.Addr) bool {
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	if ip.Is4() {
		// Shared carrier-grade NAT and special-purpose/documentation networks are
		// not valid public Trello API destinations.
		for _, prefix := range trelloNonPublicIPv4Prefixes {
			if prefix.Contains(ip) {
				return false
			}
		}
		return true
	}
	// Global unicast IPv6 space is 2000::/3. IsGlobalUnicast alone also accepts
	// several special-use ranges that must not receive connector credentials.
	if !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, prefix := range trelloNonPublicIPv6Prefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}
