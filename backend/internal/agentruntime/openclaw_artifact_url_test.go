package agentruntime

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestArtifactURLOriginPolicy(t *testing.T) {
	t.Setenv("AUTOMATION_NAT64_PREFIXES", "")
	for _, tc := range []struct {
		name, raw string
		origins   []string
		ok        bool
	}{
		{"disabled", "https://files.example/report?signature=secret", nil, false},
		{"https", "https://files.example/report?signature=secret", []string{"https://files.example"}, true},
		{"wrong port", "https://files.example:444/report", []string{"https://files.example"}, false},
		{"suffix", "https://files.example.evil/report", []string{"https://files.example"}, false},
		{"wildcard", "https://files.example/report", []string{"*"}, false},
		{"credentials", "https://user:secret@files.example/report", []string{"https://files.example"}, false},
		{"fragment", "https://files.example/report#secret", []string{"https://files.example"}, false},
		{"remote plaintext", "http://files.example/report", []string{"http://files.example"}, false},
		{"localhost is not a production origin", "http://localhost:8888/report", []string{"http://localhost:8888"}, false},
		{"private HTTPS literal", "https://10.2.3.4/report", []string{"https://10.2.3.4"}, false},
		{"uppercase scheme and host", "HTTPS://Files.Example/report", []string{"https://files.example"}, true},
		{"IDN matches its ASCII origin", "https://bücher.example/report", []string{"https://xn--bcher-kva.example"}, true},
		{"empty explicit port", "https://files.example:/report", []string{"https://files.example"}, false},
		{"literal dot traversal", "https://files.example/a/../report", []string{"https://files.example"}, false},
		{"escaped dot traversal", "https://files.example/%2e%2e/report", []string{"https://files.example"}, false},
		{"double escaped traversal", "https://files.example/%252e%252e%252fprivate", []string{"https://files.example"}, false},
		{"backslash path", "https://files.example/a%5c..%5cprivate", []string{"https://files.example"}, false},
		{"loopback explicit is not production-safe", "http://127.0.0.1:8888/report", []string{"http://127.0.0.1:8888"}, false},
		{"IPv6 loopback is not production-safe", "https://[::1]/report", []string{"https://[::1]"}, false},
		{"mapped IPv4 loopback is not production-safe", "https://[::ffff:127.0.0.1]/report", []string{"https://[::ffff:127.0.0.1]"}, false},
		{"metadata", "https://169.254.169.254/secret", []string{"https://169.254.169.254"}, false},
		{"mapped IPv6 metadata", "https://[::ffff:169.254.169.254]/secret", []string{"https://[::ffff:169.254.169.254]"}, false},
		{"IPv6 zone identifier", "https://[fe80::1%25eth0]/secret", []string{"https://[fe80::1%25eth0]"}, false},
		{"encoded path control", "https://files.example/a%00b", []string{"https://files.example"}, false},
		{"file", "file:///secret", []string{"file://"}, false},
		{"origin with path", "https://files.example/report", []string{"https://files.example/path"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateArtifactDownloadURL(tc.raw, tc.origins)
			if (err == nil) != tc.ok {
				t.Fatalf("policy verdict: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("secret leaked")
			}
		})
	}
	for _, raw := range []string{"http://127.0.0.1:8888/report", "http://[::1]:8888/report", "https://[::ffff:127.0.0.1]/report"} {
		t.Run("isolated test origin "+raw, func(t *testing.T) {
			origin, err := urlOriginForTest(raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := validateArtifactDownloadURLForIsolatedTest(raw, []string{origin}); err != nil {
				t.Fatalf("explicit isolated loopback origin rejected: %v", err)
			}
			if _, err := validateArtifactDownloadURL(raw, []string{origin}); !errors.Is(err, ErrOpenClawArtifactURL) {
				t.Fatalf("production validator accepted isolated loopback origin: %v", err)
			}
		})
	}
}

func urlOriginForTest(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	return parsed.String(), nil
}

func TestArtifactURLDNSAddressPolicy(t *testing.T) {
	t.Setenv("AUTOMATION_NAT64_PREFIXES", "")
	for _, tc := range []struct {
		host      string
		addresses []string
		ok        bool
	}{
		{"files.example", []string{"8.8.8.8"}, true},
		{"files.example", []string{"8.8.8.8", "127.0.0.1"}, false},
		{"files.example", []string{"10.0.0.1"}, false},
		{"files.example", []string{"100.100.100.200"}, false},
		{"files.example", []string{"64:ff9b::7f00:1"}, false},
		{"files.example", []string{"64:ff9b:1::a00:1"}, false},
		{"files.example", []string{"2002:0a00:0001::"}, false},
		{"localhost", []string{"127.0.0.1", "::1"}, false},
		{"127.0.0.1", []string{"127.0.0.1"}, false},
		{"::1", []string{"::1"}, false},
		{"files.example", []string{"::ffff:127.0.0.1"}, false},
		{"localhost", []string{"8.8.8.8"}, false},
		{"10.0.0.1", []string{"10.0.0.1"}, false},
		{"files.example", []string{"192.168.1.8"}, false},
		{"files.example", []string{"fd00::1"}, false},
		{"files.example", []string{"::ffff:10.0.0.1"}, false},
		{"files.example", []string{"::ffff:169.254.169.254"}, false},
		{"localhost.", []string{"8.8.8.8"}, false},
		{"127.0.0.1", []string{"169.254.169.254"}, false},
	} {
		t.Run(tc.host+strings.Join(tc.addresses, "-"), func(t *testing.T) {
			addresses := make([]net.IPAddr, 0, len(tc.addresses))
			for _, ip := range tc.addresses {
				addresses = append(addresses, net.IPAddr{IP: net.ParseIP(ip)})
			}
			if err := validateArtifactDownloadAddresses(tc.host, addresses); (err == nil) != tc.ok {
				t.Fatalf("address verdict: %v", err)
			}
		})
	}
	if err := validateArtifactDownloadAddressesWithPolicy("localhost", []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}, {IP: net.ParseIP("::1")}}, true); err != nil {
		t.Fatalf("isolated localhost should permit loopback answers: %v", err)
	}
	if err := validateArtifactDownloadAddressesWithPolicy("api.localhost", []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, true); err != nil {
		t.Fatalf("isolated localhost subdomain should permit loopback: %v", err)
	}
	if err := validateArtifactDownloadAddressesWithPolicy("localhost", []net.IPAddr{{IP: net.ParseIP("::ffff:127.0.0.1")}}, true); err != nil {
		t.Fatalf("isolated localhost should permit mapped loopback: %v", err)
	}
	if err := validateArtifactDownloadAddressesWithPolicy("localhost", []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, true); !errors.Is(err, ErrOpenClawArtifactURL) {
		t.Fatalf("isolated localhost accepted a public DNS answer: %v", err)
	}
	if err := validateArtifactDownloadAddresses("files.example", []net.IPAddr{{IP: net.ParseIP("fe80::1"), Zone: "eth0"}}); !errors.Is(err, ErrOpenClawArtifactURL) {
		t.Fatalf("scoped IPv6 address accepted: %v", err)
	}
	tooMany := make([]net.IPAddr, 17)
	for i := range tooMany {
		tooMany[i].IP = net.ParseIP("8.8.8.8")
	}
	if err := validateArtifactDownloadAddresses("files.example", tooMany); !errors.Is(err, ErrOpenClawArtifactURL) {
		t.Fatalf("oversized DNS answer set accepted: %v", err)
	}
}

type fixedArtifactResolver struct {
	answers []net.IPAddr
	next    []net.IPAddr
	err     error
	calls   int
}

func (resolver *fixedArtifactResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	resolver.calls++
	if resolver.calls > 1 && resolver.next != nil {
		return resolver.next, resolver.err
	}
	return resolver.answers, resolver.err
}

func TestArtifactURLDialPinsValidatedDNSAnswer(t *testing.T) {
	t.Setenv("AUTOMATION_NAT64_PREFIXES", "")
	resolver := &fixedArtifactResolver{
		answers: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}},
		next:    []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}},
	}
	var dialAddress string
	dialCalls := 0
	conn, err := dialValidatedArtifactAddress(
		context.Background(), "tcp", "files.example:443", "files.example", resolver, false,
		func(_ context.Context, network, address string) (net.Conn, error) {
			dialCalls++
			dialAddress = network + " " + address
			client, peer := net.Pipe()
			_ = peer.Close()
			return client, nil
		},
	)
	if err != nil {
		t.Fatalf("validated public DNS answer failed: %v", err)
	}
	_ = conn.Close()
	if resolver.calls != 1 || dialCalls != 1 || dialAddress != "tcp 8.8.8.8:443" {
		t.Fatalf("DNS result was not pinned (lookups=%d dials=%d destination=%q)", resolver.calls, dialCalls, dialAddress)
	}

	resolver.calls = 0
	resolver.next = nil
	resolver.answers = []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("127.0.0.1")}}
	if _, err := dialValidatedArtifactAddress(
		context.Background(), "tcp", "files.example:443", "files.example", resolver, false,
		func(context.Context, string, string) (net.Conn, error) {
			dialCalls++
			return nil, errors.New("unexpected dial")
		},
	); !errors.Is(err, ErrOpenClawArtifactURL) {
		t.Fatalf("mixed public/loopback DNS answer was not rejected: %v", err)
	}
	if resolver.calls != 1 || dialCalls != 1 {
		t.Fatalf("unsafe DNS answer reached dialer (lookups=%d dials=%d)", resolver.calls, dialCalls)
	}
}

func TestArtifactURLNAT64AddressPolicy(t *testing.T) {
	publicWellKnown := net.ParseIP("64:ff9b::808:808")
	privateWellKnown := net.ParseIP("64:ff9b::a00:1")
	loopbackWellKnown := net.ParseIP("64:ff9b::7f00:1")
	linkLocalWellKnown := net.ParseIP("64:ff9b::a9fe:101")
	publicConfigured := net.ParseIP("2001:4860:1234:808:8:800::")
	privateConfigured := net.ParseIP("2001:4860:1234:a00:0:100::")
	loopbackConfigured := net.ParseIP("2001:4860:1234:7f00:0:100::")
	linkLocalConfigured := net.ParseIP("2001:4860:1234:a9fe:1:100::")
	malformedConfigured := net.ParseIP("2001:4860:1234:808:8:800:1::")
	for _, tc := range []struct {
		name      string
		host      string
		addresses []net.IP
		prefixes  string
		wantErr   bool
	}{
		{name: "well-known public IPv4", host: "files.example", addresses: []net.IP{publicWellKnown}},
		{name: "well-known private IPv4", host: "files.example", addresses: []net.IP{privateWellKnown}, wantErr: true},
		{name: "well-known loopback IPv4", host: "files.example", addresses: []net.IP{loopbackWellKnown}, wantErr: true},
		{name: "well-known link-local IPv4", host: "files.example", addresses: []net.IP{linkLocalWellKnown}, wantErr: true},
		{name: "configured public IPv4", host: "files.example", addresses: []net.IP{publicConfigured}, prefixes: "2001:4860:1234::/48"},
		{name: "configured private IPv4", host: "files.example", addresses: []net.IP{privateConfigured}, prefixes: "2001:4860:1234::/48", wantErr: true},
		{name: "configured loopback IPv4", host: "files.example", addresses: []net.IP{loopbackConfigured}, prefixes: "2001:4860:1234::/48", wantErr: true},
		{name: "configured link-local IPv4", host: "files.example", addresses: []net.IP{linkLocalConfigured}, prefixes: "2001:4860:1234::/48", wantErr: true},
		{name: "configured non-zero suffix rejected", host: "files.example", addresses: []net.IP{malformedConfigured}, prefixes: "2001:4860:1234::/48", wantErr: true},
		{name: "configured mixed public and private answers", host: "files.example", addresses: []net.IP{publicConfigured, privateConfigured}, prefixes: "2001:4860:1234::/48", wantErr: true},
		{name: "native IPv6 unaffected", host: "files.example", addresses: []net.IP{net.ParseIP("2606:4700:4700::1111")}, prefixes: "2001:db8:1234::/48"},
		{name: "malformed configuration fails closed", host: "files.example", addresses: []net.IP{net.ParseIP("8.8.8.8")}, prefixes: "not-a-cidr", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AUTOMATION_NAT64_PREFIXES", tc.prefixes)
			addresses := make([]net.IPAddr, 0, len(tc.addresses))
			for _, ip := range tc.addresses {
				addresses = append(addresses, net.IPAddr{IP: ip})
			}
			if err := validateArtifactDownloadAddresses(tc.host, addresses); (err != nil) != tc.wantErr {
				t.Fatalf("address policy error = %v, wantErr=%v", err, tc.wantErr)
			}
		})
	}

	t.Setenv("AUTOMATION_NAT64_PREFIXES", "")
	if _, err := validateArtifactDownloadURL("https://[64:ff9b::808:808]/artifact", []string{"https://[64:ff9b::808:808]"}); err != nil {
		t.Fatalf("public well-known NAT64 literal origin rejected: %v", err)
	}
	if _, err := validateArtifactDownloadURL("https://[64:ff9b::a00:1]/artifact", []string{"https://[64:ff9b::a00:1]"}); !errors.Is(err, ErrOpenClawArtifactURL) {
		t.Fatalf("private well-known NAT64 literal origin accepted: %v", err)
	}
}

func TestArtifactURLResultFormattingNeverLeaksSignedURL(t *testing.T) {
	secretURL := "https://files.example/private?signature=super-secret"
	err := &openClawArtifactURLResult{url: secretURL}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		formatted := fmt.Sprintf(format, err)
		if strings.Contains(formatted, secretURL) || strings.Contains(formatted, "super-secret") {
			t.Fatalf("error format %q leaked signed URL: %q", format, formatted)
		}
	}
}

func TestArtifactURLTransportBoundedAndPrivate(t *testing.T) {
	var hits atomic.Int32
	var redirectedHits atomic.Int32
	redirectedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectedHits.Add(1)
		_, _ = w.Write([]byte("hi"))
	}))
	defer redirectedServer.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credentials forwarded")
		}
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/content", http.StatusFound)
		case "/cross-origin-redirect":
			http.Redirect(w, r, redirectedServer.URL+"/content", http.StatusFound)
		case "/oversize":
			w.Header().Set("Content-Length", "8388609")
		case "/encoded":
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write([]byte("hi"))
		case "/duplicate-encoding":
			w.Header().Add("Content-Encoding", "identity")
			w.Header().Add("Content-Encoding", "gzip")
			_, _ = w.Write([]byte("hi"))
		case "/streamed-oversize":
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte(strings.Repeat("x", OpenClawArtifactContentLimit+1)))
		case "/truncated":
			w.Header().Set("Content-Length", "10")
			_, _ = w.Write([]byte("hi"))
		case "/wrong-size":
			_, _ = w.Write([]byte("longer"))
		default:
			_, _ = w.Write([]byte("hi"))
		}
	}))
	defer server.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	size := int64(2)
	for _, path := range []string{"/content", "/redirect", "/cross-origin-redirect", "/oversize", "/encoded", "/duplicate-encoding", "/streamed-oversize", "/truncated", "/wrong-size"} {
		before := hits.Load()
		redirectedBefore := redirectedHits.Load()
		content, err := fetchOpenClawArtifactURLForIsolatedTest(context.Background(), server.URL+path+"?token=secret", []string{server.URL}, &size)
		if path == "/content" {
			if err != nil || string(content.Data) != "hi" {
				t.Fatalf("content: %v", err)
			}
		} else if err == nil {
			t.Fatalf("accepted %s", path)
		}
		if err != nil && strings.Contains(err.Error(), "secret") {
			t.Fatal("URL leaked")
		}
		if hits.Load()-before != 1 {
			t.Fatal("request replayed or redirected")
		}
		if redirectedHits.Load() != redirectedBefore {
			t.Fatal("cross-origin redirect target was contacted")
		}
	}
	before := hits.Load()
	if _, err := fetchOpenClawArtifactURL(context.Background(), server.URL+"/content", []string{server.URL}, &size); !errors.Is(err, ErrOpenClawArtifactURL) {
		t.Fatalf("production downloader accepted an isolated loopback origin: %v", err)
	}
	if hits.Load() != before {
		t.Fatal("production downloader contacted an isolated loopback origin")
	}
	for _, invalidSize := range []int64{-1, OpenClawArtifactContentLimit + 1} {
		if _, err := fetchOpenClawArtifactURLForIsolatedTest(context.Background(), server.URL+"/content", []string{server.URL}, &invalidSize); !errors.Is(err, ErrOpenClawArtifactUnsupported) {
			t.Fatalf("invalid expected size %d: %v", invalidSize, err)
		}
	}
	if hits.Load() != before {
		t.Fatal("invalid expected sizes contacted the content server")
	}
	before = hits.Load()
	if _, err := fetchOpenClawArtifactURLForIsolatedTest(context.Background(), server.URL+"/content", nil, &size); !errors.Is(err, ErrOpenClawArtifactURL) {
		t.Fatalf("unapproved origin: %v", err)
	}
	if hits.Load() != before {
		t.Fatal("unapproved URL contacted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fetchOpenClawArtifactURLForIsolatedTest(ctx, server.URL+"/content", []string{server.URL}, &size); err == nil {
		t.Fatal("cancelled request accepted")
	}
	if hits.Load() != before {
		t.Fatal("cancelled URL contacted")
	}
}

func TestArtifactURLCancellationDuringBody(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := fetchOpenClawArtifactURLForIsolatedTest(ctx, server.URL+"/?token=secret", []string{server.URL}, nil)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("download did not start")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("cancelled body result: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("body download did not cancel")
	}
}

func TestArtifactURLExpiry(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		value string
		ok    bool
	}{{"", true}, {now.Add(time.Hour).Format(time.RFC3339), true}, {now.Add(-time.Second).Format(time.RFC3339), false}, {"invalid secret", false}} {
		if artifactURLExpiryValid(tc.value, now) != tc.ok {
			t.Fatal("incorrect expiry verdict")
		}
	}
}
