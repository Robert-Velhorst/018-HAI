package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/idna"

	"automation-hub-backend/internal/nat64"
)

// URL capabilities remain private to this request; error strings and JSON must
// never expose their signed query strings.
type openClawArtifactURLResult struct {
	url        string
	expiresAt  string
	descriptor GatewayArtifactDescriptor
}

func (*openClawArtifactURLResult) Error() string { return ErrOpenClawArtifactURL.Error() }
func (*openClawArtifactURLResult) Unwrap() error { return ErrOpenClawArtifactURL }

// Format intentionally hides the signed URL even when callers log errors with
// %+v or %#v instead of Error().
func (result *openClawArtifactURLResult) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, result.Error())
}

func artifactURLExpiryValid(value string, now time.Time) bool {
	if value == "" {
		return true
	}
	expiry, err := time.Parse(time.RFC3339, value)
	return err == nil && expiry.After(now)
}

func normalizedArtifactHost(host string) (string, bool) {
	if host == "" || strings.ContainsAny(host, "%\\*") {
		return "", false
	}
	if address, err := netip.ParseAddr(host); err == nil {
		if address.Zone() != "" {
			return "", false
		}
		return strings.ToLower(address.String()), true
	}
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil {
		return "", false
	}
	ascii = strings.ToLower(strings.TrimSuffix(ascii, "."))
	if ascii == "" || len(ascii) > 253 {
		return "", false
	}
	for _, label := range strings.Split(ascii, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return "", false
			}
		}
	}
	return ascii, true
}

func artifactLocalhost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "localhost" || strings.HasSuffix(host, ".localhost")
}

func safeArtifactURLPath(path string) bool {
	// Check repeated decoding because a proxy or object store may decode a
	// signed path more than once. Reject ambiguous traversal, not ordinary
	// escaped object names.
	for depth := 0; depth < 8; depth++ {
		if strings.Contains(path, "\\") {
			return false
		}
		for _, char := range path {
			if char < 0x20 || char == 0x7f {
				return false
			}
		}
		for _, segment := range strings.Split(path, "/") {
			if segment == "." || segment == ".." {
				return false
			}
		}
		next, err := url.PathUnescape(path)
		if err != nil || next == path {
			return true
		}
		path = next
	}
	return false
}

func artifactOrigin(u *url.URL, allowLoopbackTestOrigin bool) (string, bool) {
	if u == nil || u.User != nil || u.Opaque != "" || u.Fragment != "" || u.Hostname() == "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return "", false
	}
	host, validHost := normalizedArtifactHost(u.Hostname())
	if !validHost || strings.HasSuffix(u.Host, ":") {
		return "", false
	}
	loopbackIP := false
	if address, err := netip.ParseAddr(host); err == nil {
		loopbackIP = address.Unmap().IsLoopback()
		if (loopbackIP && !allowLoopbackTestOrigin) || (!loopbackIP && blockedArtifactIP(net.ParseIP(host))) {
			return "", false
		}
	}
	loopbackHost := artifactLocalhost(host) || loopbackIP
	if loopbackHost && !allowLoopbackTestOrigin {
		return "", false
	}
	if scheme == "http" && !(allowLoopbackTestOrigin && loopbackHost) {
		return "", false
	}
	port := u.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", false
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(number)), true
}

func validateArtifactDownloadURL(raw string, origins []string) (*url.URL, error) {
	return validateArtifactDownloadURLWithPolicy(raw, origins, false)
}

// validateArtifactDownloadURLForIsolatedTest permits explicit loopback test
// servers. Production downloads must use validateArtifactDownloadURL.
func validateArtifactDownloadURLForIsolatedTest(raw string, origins []string) (*url.URL, error) {
	return validateArtifactDownloadURLWithPolicy(raw, origins, true)
}

func validateArtifactDownloadURLWithPolicy(raw string, origins []string, allowLoopbackTestOrigin bool) (*url.URL, error) {
	if len(raw) == 0 || len(raw) > 8192 {
		return nil, ErrOpenClawArtifactURL
	}
	target, err := url.Parse(raw)
	if err != nil || !safeArtifactURLPath(target.Path) {
		return nil, ErrOpenClawArtifactURL
	}
	origin, ok := artifactOrigin(target, allowLoopbackTestOrigin)
	if !ok {
		return nil, ErrOpenClawArtifactURL
	}
	target.Scheme = strings.ToLower(target.Scheme)
	for _, allowed := range origins {
		parsed, err := url.Parse(allowed)
		if err != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.ForceQuery {
			continue
		}
		approved, valid := artifactOrigin(parsed, allowLoopbackTestOrigin)
		if valid && approved == origin {
			return target, nil
		}
	}
	return nil, ErrOpenClawArtifactURL
}

var artifactBlockedRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("168.63.129.16/32"), netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("fec0::/10"), netip.MustParsePrefix("::/96"),
	// Translation/tunnel prefixes can conceal a different IPv4 destination.
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2002::/16"),
}

func blockedArtifactIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	prefixes, err := nat64.ParsePrefixes(os.Getenv("AUTOMATION_NAT64_PREFIXES"))
	return err != nil || blockedArtifactAddressWithNAT64(address.Unmap(), prefixes)
}

func blockedArtifactAddress(address netip.Addr) bool {
	if !address.IsValid() || address.IsLoopback() || address.IsUnspecified() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || !address.IsGlobalUnicast() || address.IsPrivate() {
		return true
	}
	for _, prefix := range artifactBlockedRanges {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func blockedArtifactAddressWithNAT64(address netip.Addr, prefixes []netip.Prefix) bool {
	if address.Is6() {
		for _, prefix := range artifactBlockedRanges {
			if prefix.String() != nat64.WellKnownPrefix && prefix.Contains(address) {
				return true
			}
		}
		embeddedIPv4, matched, valid := nat64.Extract(address, prefixes)
		if matched {
			return !valid || embeddedIPv4.IsLoopback() || blockedArtifactAddress(embeddedIPv4)
		}
	}
	return blockedArtifactAddress(address)
}

func validateArtifactDownloadAddresses(host string, addresses []net.IPAddr) error {
	return validateArtifactDownloadAddressesWithPolicy(host, addresses, false)
}

func validateArtifactDownloadAddressesWithPolicy(host string, addresses []net.IPAddr, allowLoopbackTestOrigin bool) error {
	prefixes, err := nat64.ParsePrefixes(os.Getenv("AUTOMATION_NAT64_PREFIXES"))
	if err != nil {
		return ErrOpenClawArtifactURL
	}
	if len(addresses) == 0 || len(addresses) > 16 {
		return ErrOpenClawArtifactURL
	}
	literal, literalErr := netip.ParseAddr(host)
	for _, address := range addresses {
		ip, ok := netip.AddrFromSlice(address.IP)
		if !ok || address.Zone != "" {
			return ErrOpenClawArtifactURL
		}
		ip = ip.Unmap()
		loopback := ip.IsLoopback()
		if (loopback && !allowLoopbackTestOrigin) || (!loopback && blockedArtifactAddressWithNAT64(ip, prefixes)) {
			return ErrOpenClawArtifactURL
		}
		switch {
		case literalErr == nil:
			if literal.Zone() != "" || literal.Unmap() != ip {
				return ErrOpenClawArtifactURL
			}
		case artifactLocalhost(host):
			if !ip.IsLoopback() {
				return ErrOpenClawArtifactURL
			}
		default:
			if ip.IsLoopback() {
				return ErrOpenClawArtifactURL
			}
		}
	}
	return nil
}

type artifactIPResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

func dialValidatedArtifactAddress(
	ctx context.Context,
	network, address, expectedHost string,
	resolver artifactIPResolver,
	allowLoopbackTestOrigin bool,
	dial func(context.Context, string, string) (net.Conn, error),
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || !strings.EqualFold(host, expectedHost) {
		return nil, ErrOpenClawArtifactURL
	}
	addresses := []net.IPAddr{}
	if ip := net.ParseIP(host); ip != nil {
		addresses = append(addresses, net.IPAddr{IP: ip})
	} else {
		addresses, err = resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("artifact host resolution failed")
		}
	}
	if err := validateArtifactDownloadAddressesWithPolicy(host, addresses, allowLoopbackTestOrigin); err != nil {
		return nil, err
	}
	// Dial the validated result directly so a later resolver answer cannot rebind the host.
	return dial(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
}

func fetchOpenClawArtifactURL(ctx context.Context, raw string, origins []string, expectedSize *int64) (*GatewayArtifactContent, error) {
	return fetchOpenClawArtifactURLWithPolicy(ctx, raw, origins, expectedSize, false)
}

// fetchOpenClawArtifactURLForIsolatedTest allows only explicit loopback
// httptest origins. It must not be used by production code.
func fetchOpenClawArtifactURLForIsolatedTest(ctx context.Context, raw string, origins []string, expectedSize *int64) (*GatewayArtifactContent, error) {
	return fetchOpenClawArtifactURLWithPolicy(ctx, raw, origins, expectedSize, true)
}

func fetchOpenClawArtifactURLWithPolicy(ctx context.Context, raw string, origins []string, expectedSize *int64, allowLoopbackTestOrigin bool) (*GatewayArtifactContent, error) {
	target, err := validateArtifactDownloadURLWithPolicy(raw, origins, allowLoopbackTestOrigin)
	if err != nil {
		return nil, err
	}
	if expectedSize != nil && (*expectedSize < 0 || *expectedSize > OpenClawArtifactContentLimit) {
		return nil, ErrOpenClawArtifactUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("artifact download cancelled")
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy: nil, DisableCompression: true, DisableKeepAlives: true,
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 64 << 10,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialValidatedArtifactAddress(ctx, network, address, target.Hostname(), net.DefaultResolver, allowLoopbackTestOrigin, dialer.DialContext)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("invalid artifact download request")
	}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, ErrOpenClawArtifactURL) {
			return nil, ErrOpenClawArtifactURL
		}
		return nil, fmt.Errorf("artifact URL transport failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("artifact URL did not return content")
	}
	encodings := response.Header.Values("Content-Encoding")
	if len(encodings) > 1 || (len(encodings) == 1 && !strings.EqualFold(strings.TrimSpace(encodings[0]), "identity")) {
		return nil, fmt.Errorf("artifact URL used unsupported content encoding")
	}
	if response.ContentLength > OpenClawArtifactContentLimit {
		return nil, ErrOpenClawArtifactUnsupported
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, OpenClawArtifactContentLimit+1))
	if err != nil {
		return nil, fmt.Errorf("artifact URL content read failed")
	}
	if len(data) > OpenClawArtifactContentLimit {
		return nil, ErrOpenClawArtifactUnsupported
	}
	if expectedSize != nil && int64(len(data)) != *expectedSize {
		return nil, fmt.Errorf("artifact URL content size mismatch")
	}
	sum := sha256.Sum256(data)
	return &GatewayArtifactContent{Data: data, ContentSHA256: hex.EncodeToString(sum[:])}, nil
}
