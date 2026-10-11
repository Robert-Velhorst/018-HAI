package accountfeed

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"automation-hub-backend/internal/pathsafety"
)

const maxFeedBytes = 5 << 20 // 5 MiB bound on a fetched feed

const feedHTTPTimeout = 10 * time.Second

type feedLookupIPAddr func(context.Context, string) ([]net.IPAddr, error)

var sharedFeedHTTPClient = newFeedHTTPClient(net.DefaultResolver.LookupIPAddr)

// FetchOptions configure feed fetching.
type FetchOptions struct {
	FeedsRoot string // allowlisted root for local file feeds
	AllowHTTP bool   // HTTP feeds only fetched when enabled
}

// fetchFeedBytes reads the raw feed bytes for a feed, confining local files to
// the feeds root and validating HTTP URLs.
func fetchFeedBytes(ctx context.Context, feed Feed, opts FetchOptions) ([]byte, error) {
	switch feed.SourceType {
	case SourceLocalJSONFile:
		if !pathsafety.IsSafeRelative(feed.Path) {
			return nil, fmt.Errorf("accountfeed: unsafe feed path %q", feed.Path)
		}
		full, err := pathsafety.SafeJoin(opts.FeedsRoot, feed.Path)
		if err != nil {
			return nil, fmt.Errorf("accountfeed: %w", err)
		}
		return os.ReadFile(full)
	case SourceHTTPJSONFeed:
		if !opts.AllowHTTP {
			return nil, fmt.Errorf("accountfeed: HTTP feeds are disabled (set the enable flag to allow %s)", feed.URL)
		}
		if err := validateFeedURL(feed.URL); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.URL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := sharedFeedHTTPClient.Do(req)
		if err != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				return nil, contextErr
			}
			return nil, fmt.Errorf("accountfeed: feed fetch failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("accountfeed: feed HTTP %d", resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBytes+1))
		if err != nil {
			return nil, fmt.Errorf("accountfeed: read feed: %w", err)
		}
		if len(body) > maxFeedBytes {
			return nil, fmt.Errorf("accountfeed: feed exceeds %d bytes", maxFeedBytes)
		}
		return body, nil
	default:
		return nil, fmt.Errorf("accountfeed: unsupported sourceType %q", feed.SourceType)
	}
}

// validateFeedURL rejects link-local, metadata, and unspecified hosts. Localhost
// is allowed only for local dev (§10.11).
func validateFeedURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("accountfeed: invalid feed URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("accountfeed: feed URL scheme must be http/https")
	}
	if u.User != nil {
		return fmt.Errorf("accountfeed: feed URL must not contain credentials")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("accountfeed: feed URL host is empty")
	}
	if strings.Contains(strings.ToLower(host), "metadata") {
		return fmt.Errorf("accountfeed: metadata host not allowed")
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && feedIPAddressBlocked(ip) {
		return fmt.Errorf("accountfeed: blocked host address")
	}
	return nil
}

func newFeedHTTPClient(lookup feedLookupIPAddr) *http.Client {
	dialer := &net.Dialer{Timeout: feedHTTPTimeout}
	transport := &http.Transport{
		Proxy:                 nil,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("accountfeed: invalid network address: %w", err)
			}
			resolved, err := lookup(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("accountfeed: resolve feed host: %w", err)
			}
			if len(resolved) == 0 {
				return nil, fmt.Errorf("accountfeed: feed host resolved to no addresses")
			}
			for _, candidate := range resolved {
				if feedIPAddressBlocked(candidate.IP) {
					return nil, fmt.Errorf("accountfeed: feed host resolved to blocked address space")
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(resolved[0].IP.String(), port))
		},
	}
	return &http.Client{
		Timeout:   feedHTTPTimeout,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func feedIPAddressBlocked(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.String() == "169.254.169.254"
}
