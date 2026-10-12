package llm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

type providerIPResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type providerDialFunc func(context.Context, string, string) (net.Conn, error)

var providerTransportResolver providerIPResolver = net.DefaultResolver

// providerHTTPClientTestHook is set only from _test.go so httptest fixtures can
// exercise provider protocols without weakening the production dial policy.
var providerHTTPClientTestHook func(Provider) *http.Client

func providerHTTPClientFor(provider Provider) *http.Client {
	if providerHTTPClientTestHook != nil {
		if client := providerHTTPClientTestHook(provider); client != nil {
			return client
		}
	}
	if provider.Local {
		return localProviderHTTPClient
	}
	return noRedirectHTTPClient()
}

func newProviderHTTPClient(allowLocal bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			Proxy:               nil,
			DialContext:         providerPinnedDialContext(allowLocal, providerTransportResolver, dialer.DialContext),
			MaxConnsPerHost:     8,
			MaxIdleConns:        32,
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}

var localProviderHTTPClient = newProviderHTTPClient(true)

func providerPinnedDialContext(allowLocal bool, resolver providerIPResolver, dial providerDialFunc) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("provider endpoint address is invalid")
		}
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			return nil, fmt.Errorf("provider endpoint port is invalid")
		}

		host = strings.TrimSuffix(strings.TrimSpace(host), ".")
		if host == "" {
			return nil, errors.New("provider endpoint host is empty")
		}
		if !allowLocal && isLocalModelHost(host) {
			return nil, errors.New("remote provider endpoint cannot connect to a local host")
		}

		addresses := make([]netip.Addr, 0, 4)
		if literal, parseErr := netip.ParseAddr(host); parseErr == nil {
			addresses = append(addresses, literal.Unmap().WithZone(""))
		} else {
			resolved, lookupErr := resolver.LookupNetIP(ctx, "ip", host)
			if lookupErr != nil {
				return nil, fmt.Errorf("provider endpoint DNS lookup failed")
			}
			for _, ip := range resolved {
				ip = ip.Unmap().WithZone("")
				if !ip.IsValid() {
					return nil, errors.New("provider endpoint DNS returned an invalid address")
				}
				addresses = append(addresses, ip)
			}
		}
		if len(addresses) == 0 {
			return nil, errors.New("provider endpoint resolved to no addresses")
		}

		for _, ip := range addresses {
			if allowLocal {
				if !isAllowedLocalProviderAddress(host, ip) {
					return nil, errors.New("local provider DNS resolved outside the configured local address scope")
				}
			} else if !isPublicProviderAddress(ip) {
				return nil, errors.New("remote provider DNS resolved to a non-public address")
			}
		}

		var lastErr error
		for _, ip := range addresses {
			if network == "tcp4" && !ip.Is4() || network == "tcp6" && !ip.Is6() {
				continue
			}
			conn, dialErr := dial(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, errors.New("provider endpoint has no address for the requested network")
	}
}

func isAllowedLocalProviderAddress(host string, ip netip.Addr) bool {
	if !isLocalModelHost(host) || !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	if host == "host.docker.internal" {
		return ip.IsLoopback() || ip.IsPrivate()
	}
	return ip.IsLoopback()
}

func isPublicProviderAddress(ip netip.Addr) bool {
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	for _, prefix := range providerNonPublicPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

var providerNonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), // Shared address space is not a public provider destination.
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}
