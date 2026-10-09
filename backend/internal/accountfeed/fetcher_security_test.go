package accountfeed

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"automation-hub-backend/internal/pathsafety"
)

func TestLocalImportRejectsLinksAcrossBothReaderPaths(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "private.json"), []byte(`[{"externalId":"private","title":"private"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symbolic link creation unavailable: %v", err)
	}
	assertLocalFeedPathRejected(t, root, "linked/private.json")
	assertLocalFeedPathRejected(t, filepath.Join(root, "linked"), "private.json")
}

func assertLocalFeedPathRejected(t *testing.T, root, name string) {
	t.Helper()
	feed := testFeed(name)
	if data, err := fetchFeedBytes(t.Context(), feed, FetchOptions{FeedsRoot: root}); len(data) != 0 || err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fetch escaped via link: bytes=%d err=%v", len(data), err)
	}
	reader, err := NewLocalFileReader(feed, root)
	if err != nil {
		t.Fatal(err)
	}
	if items, err := reader.Read(t.Context()); len(items) != 0 || err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy reader escaped via link: items=%d err=%v", len(items), err)
	}
}

func TestLocalImportReadsNestedRegularFileAndRejectsDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `[{"externalId":"one","title":"one"}]`
	if err := os.WriteFile(filepath.Join(root, "nested", "feed.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"nested/feed.json", `nested\feed.json`} {
		if data, err := fetchFeedBytes(t.Context(), testFeed(path), FetchOptions{FeedsRoot: root}); err != nil || string(data) != body {
			t.Fatalf("regular nested file refused: %v", err)
		}
	}
	if data, err := fetchFeedBytes(t.Context(), testFeed("nested"), FetchOptions{FeedsRoot: root}); err == nil || len(data) != 0 {
		t.Fatalf("directory admitted: bytes=%d err=%v", len(data), err)
	}
}

func TestCancelledLocalImportDoesNotOpenSource(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	root := t.TempDir()
	feed := testFeed("absent.json")
	if _, err := fetchFeedBytes(ctx, feed, FetchOptions{FeedsRoot: root}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled fetch reached missing file: %v", err)
	}
	reader, err := NewLocalFileReader(feed, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Read(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled reader reached missing file: %v", err)
	}
}

func TestSecurePathFailureUsesSafePublicReason(t *testing.T) {
	for _, err := range []error{pathsafety.ErrPathLink, pathsafety.ErrPathSubstituted, pathsafety.ErrUnsafePath} {
		if publicSyncError(err) != "feed path is not approved" {
			t.Fatalf("missing safe path reason for %v", err)
		}
	}
}

func TestFeedURLRejectsCredentialBearingConfiguration(t *testing.T) {
	for _, raw := range []string{
		"https://user:password@example.com/feed.json",
		"https://example.com/feed.json#private",
		"https://example.com/feed.json?%74oken=private",
		"https://example.com/feed.json?token%ZZ=private",
		"https://example.com/feed.json?API_KEY=private",
	} {
		if err := validateFeedURL(raw); err == nil {
			t.Fatal("credential-bearing feed URL admitted")
		}
	}
	if err := validateFeedURL("https://example.com/feed.json?cursor=next"); err != nil {
		t.Fatalf("non-secret query refused: %v", err)
	}
}

func TestSafeFeedHTTPClientDoesNotUseEnvironmentProxy(t *testing.T) {
	client := safeFeedHTTPClient(false)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("safe feed transport type = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("safe feed client must connect directly so destination IP validation cannot be bypassed by a proxy")
	}
	if transport.MaxResponseHeaderBytes != 64<<10 {
		t.Fatalf("feed response header limit = %d, want %d", transport.MaxResponseHeaderBytes, 64<<10)
	}
}

func TestSafeFeedDialRejectsPrivateDNSAnswerAndPinsPublicAnswer(t *testing.T) {
	privateDialCalls := 0
	_, err := dialSafeFeedAddress(t.Context(), "tcp", "feed.example:443", false,
		func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("127.0.0.1")}}, nil
		},
		func(context.Context, string, string) (net.Conn, error) {
			privateDialCalls++
			return nil, nil
		},
	)
	if err == nil || privateDialCalls != 0 {
		t.Fatalf("mixed public/private DNS answer: err=%v dial calls=%d, want reject before dial", err, privateDialCalls)
	}

	lookupCalls, dialCalls := 0, 0
	_, err = dialSafeFeedAddress(t.Context(), "tcp", "feed.example:443", false,
		func(context.Context, string) ([]net.IPAddr, error) {
			lookupCalls++
			if lookupCalls == 1 {
				return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
			}
			return []net.IPAddr{{IP: net.ParseIP("10.0.0.8")}}, nil
		},
		func(_ context.Context, _, address string) (net.Conn, error) {
			dialCalls++
			if address != "8.8.8.8:443" {
				t.Errorf("dial destination = %q, want resolved/pinned IP", address)
			}
			return nil, nil
		},
	)
	if err != nil || lookupCalls != 1 || dialCalls != 1 {
		t.Fatalf("public DNS answer: err=%v lookups=%d dials=%d", err, lookupCalls, dialCalls)
	}
	_, err = dialSafeFeedAddress(t.Context(), "tcp", "feed.example:443", false,
		func(context.Context, string) ([]net.IPAddr, error) {
			lookupCalls++
			return []net.IPAddr{{IP: net.ParseIP("10.0.0.8")}}, nil
		},
		func(context.Context, string, string) (net.Conn, error) {
			dialCalls++
			return nil, nil
		},
	)
	if err == nil || lookupCalls != 2 || dialCalls != 1 {
		t.Fatalf("changed private DNS answer was not rejected: err=%v lookups=%d dials=%d", err, lookupCalls, dialCalls)
	}
}

func TestFeedIPPolicyRejectsNonPublicAddressClasses(t *testing.T) {
	for _, address := range []string{
		"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1",
		"169.254.169.254", "168.63.129.16", "100.100.100.200", "0.0.0.0", "224.0.0.1", "255.255.255.255",
		"100.64.0.1", "::", "::1", "::7f00:1", "fc00::1", "fe80::1", "ff02::1",
		"::ffff:127.0.0.1", "2001:db8::1",
	} {
		t.Run(address, func(t *testing.T) {
			if err := validateFeedIP(net.ParseIP(address), false); err == nil {
				t.Fatalf("non-public address %s accepted", address)
			}
		})
	}
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		t.Run(address, func(t *testing.T) {
			if err := validateFeedIP(net.ParseIP(address), false); err != nil {
				t.Fatalf("public address %s rejected: %v", address, err)
			}
		})
	}
}

func TestSafeFeedDialRejectsNonPublicDNSAnswers(t *testing.T) {
	for _, address := range []string{
		"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.169.254",
		"168.63.129.16", "100.100.100.200", "0.0.0.0", "224.0.0.1", "100.64.0.1",
		"::", "::1", "::7f00:1", "fc00::1", "fe80::1", "ff02::1",
	} {
		t.Run(address, func(t *testing.T) {
			dialCalls := 0
			_, err := dialSafeFeedAddress(t.Context(), "tcp", "feed.example:443", false,
				func(context.Context, string) ([]net.IPAddr, error) {
					return []net.IPAddr{{IP: net.ParseIP(address)}}, nil
				},
				func(context.Context, string, string) (net.Conn, error) {
					dialCalls++
					return nil, nil
				},
			)
			if err == nil || dialCalls != 0 {
				t.Fatalf("DNS answer %s: err=%v dial calls=%d, want rejection before dialing", address, err, dialCalls)
			}
		})
	}
	for _, address := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		t.Run(address, func(t *testing.T) {
			dialAddress := ""
			_, err := dialSafeFeedAddress(t.Context(), "tcp", "feed.example:443", false,
				func(context.Context, string) ([]net.IPAddr, error) {
					return []net.IPAddr{{IP: net.ParseIP(address)}}, nil
				},
				func(_ context.Context, _, destination string) (net.Conn, error) {
					dialAddress = destination
					return nil, nil
				},
			)
			if err != nil || dialAddress == "" || strings.Contains(dialAddress, "feed.example") {
				t.Fatalf("public DNS answer %s: destination=%q err=%v", address, dialAddress, err)
			}
		})
	}
}

func TestFetchHTTPFeedRejectsLoopbackLiteralWithoutOptIn(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "[::1]", "localhost"} {
		t.Run(address, func(t *testing.T) {
			if err := validateFeedURLForFetch("http://"+address+"/feed.json", false); err == nil || !strings.Contains(err.Error(), "loopback") {
				t.Fatalf("loopback address accepted without opt-in: %v", err)
			}
		})
	}
}

func TestLoopbackHTTPRequiresExactURLOptIn(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = writer.Write([]byte(`[]`))
	}))
	defer server.Close()
	feed := testFeed("")
	feed.SourceType = SourceHTTPJSONFeed
	feed.URL = server.URL + "/feed.json"

	for _, optIn := range []string{"", server.URL, strings.Replace(feed.URL, "127.0.0.1", "localhost", 1)} {
		if _, err := fetchFeedBytes(t.Context(), feed, FetchOptions{AllowHTTP: true, AllowLoopbackURL: optIn}); err == nil {
			t.Fatalf("non-exact loopback opt-in %q accepted", optIn)
		}
	}
	if requests != 0 {
		t.Fatalf("loopback server received %d requests before exact opt-in", requests)
	}
	body, err := fetchFeedBytes(t.Context(), feed, FetchOptions{AllowHTTP: true, AllowLoopbackURL: feed.URL})
	if err != nil || string(body) != `[]` || requests != 1 {
		t.Fatalf("exact local feed opt-in: body=%q requests=%d err=%v", body, requests, err)
	}
	feed.URL = "http://192.168.1.10/feed.json"
	if _, err := fetchFeedBytes(t.Context(), feed, FetchOptions{AllowHTTP: true, AllowLoopbackURL: feed.URL}); err == nil {
		t.Fatal("exact loopback opt-in allowed a private-network URL")
	}
}

func TestFeedURLRejectsCarrierGradeNATAddresses(t *testing.T) {
	for _, raw := range []string{
		"http://100.64.0.1/feed.json",
		"http://100.127.255.254/feed.json",
	} {
		if err := validateFeedURL(raw); err == nil || !strings.Contains(err.Error(), "shared-address") {
			t.Fatalf("validateFeedURL(%q) error = %v, want shared-address rejection", raw, err)
		}
	}
	if err := validateFeedURL("https://100.128.0.1/feed.json"); err != nil {
		t.Fatalf("address just outside shared range rejected: %v", err)
	}
}

func TestFeedURLRejectsReservedAddressRanges(t *testing.T) {
	for _, raw := range []string{
		"http://192.0.2.10/feed.json",
		"http://198.18.0.1/feed.json",
		"http://203.0.113.5/feed.json",
		"http://[2001:db8::1]/feed.json",
		"http://[64:ff9b::7f00:1]/feed.json",
		"http://[64:ff9b:1::1]/feed.json",
		"http://[2002:0808:0808::1]/feed.json",
		"http://[3fff::1]/feed.json",
		"http://[fec0::1]/feed.json",
	} {
		if err := validateFeedURL(raw); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("validateFeedURL(%q) error = %v, want reserved-address rejection", raw, err)
		}
	}
	for _, raw := range []string{
		"https://8.8.8.8/feed.json",
		"https://[2606:4700:4700::1111]/feed.json",
	} {
		if err := validateFeedURL(raw); err != nil {
			t.Errorf("public address %q rejected: %v", raw, err)
		}
	}
}
