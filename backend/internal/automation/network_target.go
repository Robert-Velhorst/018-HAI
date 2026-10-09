package automation

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"automation-hub-backend/internal/nat64"
)

type automationIPLookup func(context.Context, string, string) ([]net.IP, error)

var automationSpecialPurposeNetworks = mustParseAutomationNetworks(
	"100.64.0.0/10",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"240.0.0.0/4",
	"64:ff9b:1::/48",
	"100::/64",
	"100:0:0:1::/64",
	"2001::/32",
	"2001:db8::/32",
	"2002::/16",
)

func resolveAutomationTargetAddresses(
	ctx context.Context,
	host string,
	allowLinkLocal bool,
	lookup automationIPLookup,
) ([]net.IP, error) {
	host = normalizeAutomationHost(host)
	if host == "" {
		return nil, errors.New("empty automation target host")
	}
	if lookup == nil {
		return nil, errors.New("automation target resolver is unavailable")
	}
	nat64Prefixes, err := nat64.ParsePrefixes(os.Getenv("AUTOMATION_NAT64_PREFIXES"))
	if err != nil {
		return nil, err
	}

	var addresses []net.IP
	if literal := net.ParseIP(host); literal != nil {
		addresses = []net.IP{literal}
	} else {
		resolved, err := lookup(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve automation target: %w", err)
		}
		addresses = resolved
	}
	if len(addresses) == 0 {
		return nil, errors.New("automation target resolved to no addresses")
	}

	allowPrivate := isBuiltInPrivateAutomationHost(host)
	allowLoopback := host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
	seen := make(map[string]struct{}, len(addresses))
	validated := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		if address == nil {
			return nil, errors.New("automation target resolved to an invalid address")
		}
		ip := append(net.IP(nil), address...)
		if ipv4 := ip.To4(); ipv4 != nil {
			ip = ipv4
		}
		if !automationAddressAllowed(host, ip, allowPrivate, allowLoopback, allowLinkLocal, nat64Prefixes) {
			return nil, fmt.Errorf("automation target resolved to a disallowed address class")
		}
		key := ip.String()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		validated = append(validated, ip)
	}
	return validated, nil
}

func automationAddressAllowed(host string, ip net.IP, allowPrivate, allowLoopback, allowLinkLocal bool, nat64Prefixes []netip.Prefix) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	if ip.To4() == nil {
		if ipv6 := ip.To16(); ipv6 != nil {
			address, ok := netip.AddrFromSlice(ipv6)
			if ok {
				embeddedIPv4, matched, valid := nat64.Extract(address, nat64Prefixes)
				if matched {
					return valid && automationAddressAllowed(host, net.IP(embeddedIPv4.AsSlice()), allowPrivate, allowLoopback, allowLinkLocal, nil)
				}
			}
		}
	}
	if ip.IsLinkLocalUnicast() {
		return allowLinkLocal
	}
	if ip.IsLoopback() {
		return allowLoopback
	}
	if ip.IsPrivate() {
		return allowPrivate || net.ParseIP(host) != nil
	}
	if !ip.IsGlobalUnicast() {
		return false
	}
	for _, network := range automationSpecialPurposeNetworks {
		if network.Contains(ip) {
			return false
		}
	}
	return true
}

func isBuiltInPrivateAutomationHost(host string) bool {
	switch normalizeAutomationHost(host) {
	case "backend", "frontend", "gateway", "generic-auto", "idp":
		return true
	default:
		return false
	}
}

func normalizeAutomationHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func automationHostPort(host string, port int) string {
	return net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))
}

func mustParseAutomationNetworks(values ...string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			panic("invalid built-in automation network: " + value)
		}
		networks = append(networks, network)
	}
	return networks
}

func mustParseAutomationNetwork(value string) *net.IPNet {
	_, network, err := net.ParseCIDR(value)
	if err != nil {
		panic("invalid built-in automation network: " + value)
	}
	return network
}

func dialAutomationTarget(ctx context.Context, network, address, targetHost string, vettedIPs []net.IP) (net.Conn, error) {
	requestedHost, port, err := net.SplitHostPort(address)
	if err != nil || normalizeAutomationHost(requestedHost) != normalizeAutomationHost(targetHost) {
		return nil, errors.New("automation transport requested an unexpected destination")
	}
	if len(vettedIPs) == 0 {
		return nil, errors.New("automation target has no vetted addresses")
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, ip := range vettedIPs {
		if ip == nil {
			return nil, errors.New("automation target contains an invalid vetted address")
		}
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

func noRedirectHTTPClientForTarget(timeout time.Duration, host string, vettedIPs []net.IP) *http.Client {
	ips := make([]net.IP, len(vettedIPs))
	for i, ip := range vettedIPs {
		ips[i] = append(net.IP(nil), ip...)
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialAutomationTarget(ctx, network, address, host, ips)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
