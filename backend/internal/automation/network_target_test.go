package automation

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"automation-hub-backend/internal/nat64"
)

func TestResolveAutomationTargetAddressesEnforcesDNSAddressPolicy(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		answers    []net.IP
		allowLocal bool
		wantErr    bool
	}{
		{name: "public address", host: "api.example.test", answers: []net.IP{net.ParseIP("8.8.8.8")}},
		{name: "public with private mixed answer fails closed", host: "api.example.test", answers: []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("10.0.0.5")}, wantErr: true},
		{name: "DNS loopback fails", host: "api.example.test", answers: []net.IP{net.ParseIP("127.0.0.1")}, wantErr: true},
		{name: "DNS private fails for external host", host: "api.example.test", answers: []net.IP{net.ParseIP("192.168.1.10")}, wantErr: true},
		{name: "DNS link local fails by default", host: "api.example.test", answers: []net.IP{net.ParseIP("169.254.169.254")}, wantErr: true},
		{name: "DNS link local needs explicit override", host: "api.example.test", answers: []net.IP{net.ParseIP("169.254.10.4")}, allowLocal: true},
		{name: "unspecified always fails", host: "api.example.test", answers: []net.IP{net.ParseIP("0.0.0.0")}, allowLocal: true, wantErr: true},
		{name: "localhost may resolve loopback", host: "localhost", answers: []net.IP{net.ParseIP("::1"), net.ParseIP("127.0.0.1")}},
		{name: "native public IPv6", host: "api.example.test", answers: []net.IP{net.ParseIP("2606:4700:4700::1111")}},
		{name: "built in service may resolve private", host: "gateway", answers: []net.IP{net.ParseIP("172.20.0.2")}},
		{name: "built in service may not resolve loopback", host: "gateway", answers: []net.IP{net.ParseIP("127.0.0.1")}, wantErr: true},
		{name: "literal loopback remains supported", host: "127.0.0.1", answers: nil},
		{name: "shared address space is not public", host: "api.example.test", answers: []net.IP{net.ParseIP("100.64.0.1")}, wantErr: true},
		{name: "private use NAT64 prefix is not public", host: "api.example.test", answers: []net.IP{net.ParseIP("64:ff9b:1::1")}, wantErr: true},
		{name: "well known NAT64 to metadata is blocked", host: "api.example.test", answers: []net.IP{net.ParseIP("64:ff9b::a9fe:a9fe")}, wantErr: true},
		{name: "well known NAT64 to loopback is blocked", host: "api.example.test", answers: []net.IP{net.ParseIP("64:ff9b::7f00:1")}, wantErr: true},
		{name: "well known NAT64 to link local is blocked", host: "api.example.test", answers: []net.IP{net.ParseIP("64:ff9b::a9fe:101")}, wantErr: true},
		{name: "well known NAT64 to public IPv4 is allowed", host: "api.example.test", answers: []net.IP{net.ParseIP("64:ff9b::808:808")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AUTOMATION_NAT64_PREFIXES", "")
			lookupCalls := 0
			lookup := func(context.Context, string, string) ([]net.IP, error) {
				lookupCalls++
				return tt.answers, nil
			}
			got, err := resolveAutomationTargetAddresses(context.Background(), tt.host, tt.allowLocal, lookup)
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveAutomationTargetAddresses() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && len(got) == 0 {
				t.Fatal("successful resolution returned no addresses")
			}
			if net.ParseIP(tt.host) != nil && lookupCalls != 0 {
				t.Fatalf("literal IP unexpectedly used resolver %d times", lookupCalls)
			}
		})
	}
}

func TestResolveAutomationTargetAddressesFailsClosedOnResolverErrors(t *testing.T) {
	t.Setenv("AUTOMATION_NAT64_PREFIXES", "")
	lookupErr := func(context.Context, string, string) ([]net.IP, error) { return nil, errors.New("fixture") }
	if _, err := resolveAutomationTargetAddresses(context.Background(), "api.example.test", false, lookupErr); err == nil {
		t.Fatal("resolver error was accepted")
	}
	lookupEmpty := func(context.Context, string, string) ([]net.IP, error) { return nil, nil }
	if _, err := resolveAutomationTargetAddresses(context.Background(), "api.example.test", false, lookupEmpty); err == nil {
		t.Fatal("empty DNS answer was accepted")
	}
}

func TestResolveAutomationTargetAddressesFailsClosedOnMalformedNAT64Configuration(t *testing.T) {
	t.Setenv("AUTOMATION_NAT64_PREFIXES", "2001:db8::/33")
	lookupCalled := false
	lookup := func(context.Context, string, string) ([]net.IP, error) {
		lookupCalled = true
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	}
	if _, err := resolveAutomationTargetAddresses(context.Background(), "api.example.test", false, lookup); err == nil {
		t.Fatal("resolver accepted malformed NAT64 configuration")
	}
	if lookupCalled {
		t.Fatal("resolver was called before invalid NAT64 configuration was rejected")
	}
}

func TestPinnedAutomationHTTPClientUsesVettedIPAndIgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("AUTOMATION_NAT64_PREFIXES", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Host, "api.example.test:") {
			t.Errorf("Host = %q, want original hostname and port", r.Host)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	lookupCalls := 0
	lookup := func(context.Context, string, string) ([]net.IP, error) {
		lookupCalls++
		if lookupCalls == 1 {
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		}
		return []net.IP{net.ParseIP("169.254.169.254")}, nil
	}
	// A literal custom resolver makes the test deterministic; this test focuses
	// on the HTTP transport pin, while the policy test above checks public DNS.
	addresses := []net.IP{net.ParseIP("127.0.0.1")}
	if _, err := resolveAutomationTargetAddresses(context.Background(), "localhost", false, lookup); err != nil {
		t.Fatalf("resolve localhost control: %v", err)
	}
	if lookupCalls != 1 {
		t.Fatalf("resolver called %d times, want 1", lookupCalls)
	}
	client := noRedirectHTTPClientForTarget(server.Client().Timeout, "api.example.test", addresses)
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		t.Setenv(key, "http://127.0.0.1:1")
	}
	response, err := client.Get("http://api.example.test:" + port + "/health")
	if err != nil {
		t.Fatalf("pinned request failed: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
}

func TestPinnedAutomationHTTPClientRejectsDifferentTransportHost(t *testing.T) {
	client := noRedirectHTTPClientForTarget(1, "api.example.test", []net.IP{net.ParseIP("8.8.8.8")})
	_, err := client.Transport.(*http.Transport).DialContext(context.Background(), "tcp", "other.example.test:80")
	if err == nil || !strings.Contains(err.Error(), "unexpected destination") {
		t.Fatalf("DialContext error = %v, want unexpected destination", err)
	}
}

func TestAutomationHostPortFormatsIPv6AndIPv4(t *testing.T) {
	if got := automationHostPort("::1", 8080); got != "[::1]:8080" {
		t.Fatalf("IPv6 target = %q, want [::1]:8080", got)
	}
	if got := automationHostPort("[::1]", 8080); got != "[::1]:8080" {
		t.Fatalf("bracketed IPv6 target = %q, want [::1]:8080", got)
	}
	if got := automationHostPort("127.0.0.1", 8080); got != "127.0.0.1:8080" {
		t.Fatalf("IPv4 target = %q, want 127.0.0.1:8080", got)
	}
}

func TestDialAutomationTargetSupportsIPv6Loopback(t *testing.T) {
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback is unavailable: %v", err)
	}
	defer listener.Close()
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			_ = conn.Close()
			close(accepted)
		}
	}()

	host := "::1"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := dialAutomationTarget(ctx, "tcp", automationHostPort(host, port), host, []net.IP{net.ParseIP(host)})
	if err != nil {
		t.Fatalf("dial IPv6 loopback: %v", err)
	}
	_ = conn.Close()
	select {
	case <-accepted:
	case <-ctx.Done():
		t.Fatal("IPv6 listener did not accept the pinned connection")
	}
}

func TestConfiguredAutomationNAT64PrefixesApplyIPv4AddressPolicy(t *testing.T) {
	t.Setenv("AUTOMATION_NAT64_PREFIXES", "2001:4860:abcd::/48")
	prefixes, err := nat64.ParsePrefixes("2001:4860:abcd::/48")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		ipv4 string
		ok   bool
	}{
		{name: "private", ipv4: "10.0.0.5"},
		{name: "loopback", ipv4: "127.0.0.1"},
		{name: "link-local", ipv4: "169.254.1.1"},
		{name: "public", ipv4: "8.8.8.8", ok: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			translated, err := testEmbedAutomationNAT64("2001:4860:abcd::/48", net.ParseIP(tt.ipv4))
			if err != nil {
				t.Fatal(err)
			}
			if got := automationAddressAllowed("api.example.test", translated, false, false, false, prefixes); got != tt.ok {
				t.Fatalf("automationAddressAllowed(%s) = %v, want %v", translated, got, tt.ok)
			}
		})
	}
	private, err := testEmbedAutomationNAT64("2001:4860:abcd::/48", net.ParseIP("10.0.0.5"))
	if err != nil {
		t.Fatal(err)
	}
	public, err := testEmbedAutomationNAT64("2001:4860:abcd::/48", net.ParseIP("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(_ context.Context, _ string, host string) ([]net.IP, error) {
		if host != "api.example.test" {
			t.Fatalf("unexpected lookup for %q", host)
		}
		return []net.IP{private}, nil
	}
	if _, err := resolveAutomationTargetAddresses(context.Background(), "api.example.test", false, lookup); err == nil {
		t.Fatal("resolver accepted a private IPv4 destination embedded in configured NAT64")
	}
	mixedLookup := func(context.Context, string, string) ([]net.IP, error) { return []net.IP{public, private}, nil }
	if _, err := resolveAutomationTargetAddresses(context.Background(), "api.example.test", false, mixedLookup); err == nil {
		t.Fatal("resolver accepted a mixed public and private configured NAT64 answer set")
	}
}

func TestAutomationNAT64ExtractionSupportsRFC6052PrefixLengths(t *testing.T) {
	tests := []struct {
		prefix string
		want   string
	}{
		{prefix: "2001:4860::/32"},
		{prefix: "2001:4860:1000::/40"},
		{prefix: "2001:4860:1234::/48"},
		{prefix: "2001:db8:122:300::/56", want: "2001:db8:122:3c0:0:221::"},
		{prefix: "2001:db8:122:344::/64", want: "2001:db8:122:344:c0:2:2100::"},
		{prefix: "2001:4860:abcd::/96"},
	}
	for _, tt := range tests {
		t.Run(tt.prefix, func(t *testing.T) {
			_, prefix, err := net.ParseCIDR(tt.prefix)
			if err != nil {
				t.Fatal(err)
			}
			address, err := testEmbedAutomationNAT64(tt.prefix, net.ParseIP("192.0.2.33"))
			if err != nil {
				t.Fatal(err)
			}
			if tt.want != "" && !address.Equal(net.ParseIP(tt.want)) {
				t.Fatalf("embedded address = %s, want RFC 6052 vector %s", address, tt.want)
			}
			prefixAddress, ok := netip.AddrFromSlice(prefix.IP.To16())
			if !ok {
				t.Fatal("failed to convert test prefix")
			}
			bits, _ := prefix.Mask.Size()
			parsedPrefix := netip.PrefixFrom(prefixAddress, bits)
			embedded, matched, valid := nat64.Extract(netip.MustParseAddr(address.String()), []netip.Prefix{parsedPrefix})
			if !matched || !valid || embedded != netip.MustParseAddr("192.0.2.33") {
				t.Fatalf("extracted address = %v, matched = %v, valid = %v; want 192.0.2.33", embedded, matched, valid)
			}
		})
	}
}

func TestAutomationNAT64ConfigurationRejectsMalformedAndUnsupportedPrefixes(t *testing.T) {
	for _, value := range []string{"not-a-cidr", "10.0.0.0/8", "2001:4860::/33", "2001:4860::/128"} {
		t.Run(value, func(t *testing.T) {
			if _, err := nat64.ParsePrefixes(value); err == nil {
				t.Fatalf("nat64.ParsePrefixes(%q) succeeded", value)
			}
		})
	}
}

func testEmbedAutomationNAT64(prefixText string, ipv4 net.IP) (net.IP, error) {
	_, prefix, err := net.ParseCIDR(prefixText)
	if err != nil {
		return nil, err
	}
	ipv4 = ipv4.To4()
	if ipv4 == nil || ipv4.To4() == nil {
		return nil, errors.New("test NAT64 input must be an IPv4 address")
	}
	address := append(net.IP(nil), prefix.IP.To16()...)
	bits, _ := prefix.Mask.Size()
	switch bits {
	case 32:
		copy(address[4:8], ipv4)
	case 40:
		copy(address[5:8], ipv4[:3])
		address[9] = ipv4[3]
	case 48:
		copy(address[6:8], ipv4[:2])
		copy(address[9:11], ipv4[2:])
	case 56:
		address[7] = ipv4[0]
		copy(address[9:12], ipv4[1:])
	case 64:
		copy(address[9:13], ipv4)
	case 96:
		copy(address[12:16], ipv4)
	default:
		return nil, errors.New("unsupported test NAT64 prefix length")
	}
	return address, nil
}
