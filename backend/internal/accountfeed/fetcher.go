package accountfeed

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"automation-hub-backend/internal/pathsafety"
)

const maxFeedBytes = 5 << 20 // 5 MiB bound on a fetched feed

// FetchOptions configure feed fetching.
type FetchOptions struct {
	FeedsRoot        string // allowlisted root for local file feeds
	AllowHTTP        bool   // HTTP feeds only fetched when enabled
	AllowLoopbackURL string // exact feed URL allowed to target loopback for local development
}

// fetchFeedBytes reads the raw feed bytes for a feed, confining local files to
// the feeds root and validating HTTP URLs.
func fetchFeedBytes(ctx context.Context, feed Feed, opts FetchOptions) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch feed.SourceType {
	case SourceLocalJSONFile:
		return readBoundedLocalFeed(ctx, opts.FeedsRoot, feed.Path)
	case SourceHTTPJSONFeed:
		if !opts.AllowHTTP {
			return nil, fmt.Errorf("accountfeed: HTTP feeds are disabled (set the enable flag to allow %s)", feed.URL)
		}
		allowLoopback := opts.AllowLoopbackURL != "" && opts.AllowLoopbackURL == feed.URL
		if err := validateFeedURLForFetch(feed.URL, allowLoopback); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.URL, nil)
		if err != nil {
			return nil, err
		}
		client := safeFeedHTTPClient(allowLoopback)
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("accountfeed: feed fetch failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("accountfeed: feed HTTP %d", resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBytes+1))
		if err != nil {
			return nil, err
		}
		if len(body) > maxFeedBytes {
			return nil, fmt.Errorf("accountfeed: HTTP feed exceeds %d bytes", maxFeedBytes)
		}
		return body, nil
	default:
		return nil, fmt.Errorf("accountfeed: unsupported sourceType %q", feed.SourceType)
	}
}

func readBoundedLocalFeed(ctx context.Context, base, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !pathsafety.IsSafeRelative(name) {
		return nil, fmt.Errorf("accountfeed: unsafe feed path")
	}
	root, err := pathsafety.OpenSecureRoot(base, false)
	if err != nil {
		return nil, fmt.Errorf("accountfeed: unsafe feed path: %w", err)
	}
	defer root.Close()
	name = filepath.FromSlash(strings.ReplaceAll(name, `\`, "/"))
	file, info, err := root.OpenExistingFile(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if info.Size() > maxFeedBytes {
		return nil, fmt.Errorf("accountfeed: local feed exceeds %d bytes", maxFeedBytes)
	}
	body, err := io.ReadAll(io.LimitReader(file, maxFeedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxFeedBytes {
		return nil, fmt.Errorf("accountfeed: local feed exceeds %d bytes", maxFeedBytes)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := root.VerifyFile(name, file, info); err != nil {
		return nil, fmt.Errorf("accountfeed: unsafe feed path: %w", err)
	}
	return body, nil
}

// validateFeedURL validates stored feed configuration. Loopback may be stored
// for local development, but fetchFeedBytes requires an exact URL opt-in before
// any outbound connection is made.
func validateFeedURL(raw string) error {
	return validateFeedURLForFetch(raw, true)
}

func validateFeedURLForFetch(raw string, allowLoopback bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("accountfeed: invalid feed URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("accountfeed: feed URL scheme must be http/https")
	}
	if u.User != nil || u.Fragment != "" {
		return fmt.Errorf("accountfeed: feed URL must not contain credentials or fragments")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return fmt.Errorf("accountfeed: invalid feed URL query")
	}
	for key := range query {
		if sourceURISecret.MatchString(key + "=") {
			return fmt.Errorf("accountfeed: feed URL must not contain credentials")
		}
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("accountfeed: feed URL host is empty")
	}
	if strings.Contains(host, "%") {
		return fmt.Errorf("accountfeed: scoped IP hosts are not allowed")
	}
	if strings.Contains(strings.ToLower(host), "metadata") {
		return fmt.Errorf("accountfeed: metadata host not allowed")
	}
	if strings.EqualFold(host, "localhost") {
		if allowLoopback {
			return nil
		}
		return fmt.Errorf("accountfeed: loopback host not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		return validateFeedIP(ip, allowLoopback)
	}
	return nil
}

// safeFeedHTTPClient resolves and dials only safe addresses. Validating the
// URL string alone is not enough: a hostname can resolve to an internal IP or
// change resolution between validation and connection. Redirects are disabled
// too, so one approved URL cannot bounce the worker to a different host.
func safeFeedHTTPClient(allowLoopback bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// A proxy receives the original destination URL and resolves it itself, so
	// DialContext would only validate the proxy address and could not enforce
	// the feed-host IP policy. Feed requests must connect directly.
	transport.Proxy = nil
	transport.MaxResponseHeaderBytes = 64 << 10
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialSafeFeedAddress(ctx, network, address, allowLoopback,
			net.DefaultResolver.LookupIPAddr, dialer.DialContext)
	}
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func dialSafeFeedAddress(
	ctx context.Context,
	network, address string,
	allowLoopback bool,
	lookupIP func(context.Context, string) ([]net.IPAddr, error),
	dial func(context.Context, string, string) (net.Conn, error),
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		if err := validateFeedIP(ip, allowLoopback); err != nil {
			return nil, err
		}
		return dial(ctx, network, address)
	}
	addresses, err := lookupIP(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("accountfeed: resolve feed host: %w", err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("accountfeed: feed host resolved to no addresses")
	}
	for _, resolved := range addresses {
		if err := validateFeedIP(resolved.IP, allowLoopback && strings.EqualFold(host, "localhost")); err != nil {
			return nil, err
		}
	}
	return dial(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
}

func validateFeedIP(ip net.IP, allowLoopback bool) error {
	if ip == nil {
		return fmt.Errorf("accountfeed: invalid IP address")
	}
	if v4 := ip.To4(); v4 != nil {
		addr, ok := netip.AddrFromSlice(v4)
		if ok && inFeedDeniedPrefix(addr, deniedFeedIPv4Prefixes) {
			return fmt.Errorf("accountfeed: reserved IPv4 host not allowed")
		}
	} else if addr, ok := netip.AddrFromSlice(ip); ok {
		if inFeedDeniedPrefix(addr, deniedFeedIPv6Prefixes) ||
			(addr.Is6() && !addr.IsLoopback() && netip.MustParsePrefix("::/96").Contains(addr)) {
			return fmt.Errorf("accountfeed: reserved IPv6 host not allowed")
		}
	}
	if isFeedMetadataIP(ip) {
		return fmt.Errorf("accountfeed: metadata host not allowed")
	}
	if isCarrierGradeNAT(ip) {
		return fmt.Errorf("accountfeed: shared-address host not allowed")
	}
	switch {
	case ip.IsLoopback():
		if allowLoopback {
			return nil
		}
		return fmt.Errorf("accountfeed: loopback host not allowed")
	case ip.IsUnspecified():
		return fmt.Errorf("accountfeed: unspecified host not allowed")
	case ip.IsPrivate():
		return fmt.Errorf("accountfeed: private-network host not allowed")
	case ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast():
		return fmt.Errorf("accountfeed: link-local host not allowed")
	case ip.IsMulticast():
		return fmt.Errorf("accountfeed: multicast host not allowed")
	}
	return nil
}

var (
	deniedFeedIPv4Prefixes = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("192.88.99.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("240.0.0.0/4"),
	}
	// Special-use and transition prefixes are rejected because they are not
	// ordinary public endpoints and may translate or tunnel to private targets.
	deniedFeedIPv6Prefixes = []netip.Prefix{
		netip.MustParsePrefix("64:ff9b::/96"),
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("2001::/23"),
		netip.MustParsePrefix("2002::/16"),
		netip.MustParsePrefix("64:ff9b:1::/48"),
		netip.MustParsePrefix("3fff::/20"),
		netip.MustParsePrefix("fec0::/10"),
	}
)

func inFeedDeniedPrefix(address netip.Addr, prefixes []netip.Prefix) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func isCarrierGradeNAT(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 0x40
}

func isFeedMetadataIP(ip net.IP) bool {
	return ip.Equal(net.ParseIP("169.254.169.254")) ||
		ip.Equal(net.ParseIP("168.63.129.16")) ||
		ip.Equal(net.ParseIP("100.100.100.200"))
}
