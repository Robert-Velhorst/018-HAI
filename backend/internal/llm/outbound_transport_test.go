package llm

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

func init() {
	providerHTTPClientTestHook = func(provider Provider) *http.Client {
		parsed, err := url.Parse(strings.TrimSpace(provider.EndpointURL))
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil
		}
		host := strings.Trim(strings.ToLower(parsed.Hostname()), "[]")
		if host != "localhost" && host != "::1" && net.ParseIP(host) == nil {
			return nil
		}
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil
		}
		return localProviderHTTPClient
	}
}

type fixedProviderResolver struct {
	addresses []netip.Addr
	err       error
	host      string
}

func (r *fixedProviderResolver) LookupNetIP(_ context.Context, network, host string) ([]netip.Addr, error) {
	if network != "ip" {
		return nil, errors.New("unexpected lookup network")
	}
	r.host = host
	return append([]netip.Addr(nil), r.addresses...), r.err
}

func TestProviderPinnedDialContextRejectsAnyUnsafeRemoteDNSAnswer(t *testing.T) {
	for _, test := range []struct {
		name      string
		addresses []netip.Addr
	}{
		{name: "private", addresses: []netip.Addr{netip.MustParseAddr("198.51.100.7"), netip.MustParseAddr("10.2.3.4")}},
		{name: "loopback", addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("127.0.0.1")}},
		{name: "link-local metadata", addresses: []netip.Addr{netip.MustParseAddr("169.254.169.254")}},
		{name: "shared metadata", addresses: []netip.Addr{netip.MustParseAddr("100.100.100.200")}},
		{name: "IPv6 documentation range", addresses: []netip.Addr{netip.MustParseAddr("2001:db8::5")}},
		{name: "IPv6 NAT64 private mapping", addresses: []netip.Addr{netip.MustParseAddr("64:ff9b::a00:1")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolver := &fixedProviderResolver{addresses: test.addresses}
			dialCalls := 0
			dial := func(context.Context, string, string) (net.Conn, error) {
				dialCalls++
				return nil, errors.New("dial must not be reached")
			}
			_, err := providerPinnedDialContext(false, resolver, dial)(context.Background(), "tcp", "models.example.test:443")
			if err == nil || !strings.Contains(err.Error(), "non-public address") {
				t.Fatalf("dial error = %v, want non-public DNS rejection", err)
			}
			if dialCalls != 0 {
				t.Fatalf("dial invoked %d times for unsafe DNS answer", dialCalls)
			}
		})
	}
}

func TestProviderPinnedDialContextPinsPublicDNSAnswer(t *testing.T) {
	resolver := &fixedProviderResolver{addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("8.8.8.8")}}
	var dialed string
	dial := func(_ context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" {
			t.Fatalf("network = %q, want tcp", network)
		}
		dialed = address
		client, peer := net.Pipe()
		_ = peer.Close()
		return client, nil
	}
	conn, err := providerPinnedDialContext(false, resolver, dial)(context.Background(), "tcp", "models.example.test:443")
	if err != nil {
		t.Fatalf("pinned dial: %v", err)
	}
	_ = conn.Close()
	if resolver.host != "models.example.test" {
		t.Fatalf("resolved host = %q", resolver.host)
	}
	if dialed != "1.1.1.1:443" && dialed != "8.8.8.8:443" {
		t.Fatalf("dialed %q, want one validated IP", dialed)
	}
}

func TestProviderPinnedDialContextAllowsOnlyLocalProviderAddressScope(t *testing.T) {
	tests := []struct {
		name      string
		host      string
		addresses []netip.Addr
		wantErr   bool
	}{
		{name: "localhost loopback", host: "localhost", addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		{name: "docker gateway private", host: "host.docker.internal", addresses: []netip.Addr{netip.MustParseAddr("192.168.65.254")}},
		{name: "localhost rebinding to public", host: "localhost", addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1")}, wantErr: true},
		{name: "arbitrary name resolving loopback", host: "models.example.test", addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &fixedProviderResolver{addresses: test.addresses}
			dialCalls := 0
			dial := func(context.Context, string, string) (net.Conn, error) {
				dialCalls++
				client, peer := net.Pipe()
				_ = peer.Close()
				return client, nil
			}
			conn, err := providerPinnedDialContext(true, resolver, dial)(context.Background(), "tcp", net.JoinHostPort(test.host, "11434"))
			if test.wantErr {
				if err == nil || dialCalls != 0 {
					t.Fatalf("got conn=%v err=%v dialCalls=%d; want rejection before dialing", conn, err, dialCalls)
				}
				return
			}
			if err != nil {
				t.Fatalf("local dial rejected: %v", err)
			}
			_ = conn.Close()
			if dialCalls != 1 {
				t.Fatalf("dial calls = %d, want 1", dialCalls)
			}
		})
	}
}

func TestProviderPinnedDialContextKeepsTLSHostnameVerification(t *testing.T) {
	server, roots := newTestProviderTLSServer(t)
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}

	resolver := &fixedProviderResolver{addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}
	dialer := &net.Dialer{Timeout: time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: providerPinnedDialContext(true, resolver, dialer.DialContext),
		TLSClientConfig: &tls.Config{RootCAs: roots},
	}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	defer transport.CloseIdleConnections()

	response, err := client.Get("https://localhost:" + port + "/")
	if err != nil {
		t.Fatalf("TLS request with hostname-based certificate failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	if resolver.host != "localhost" {
		t.Fatalf("DNS lookup host = %q, want localhost", resolver.host)
	}
}

func TestProviderHTTPClientTestHookIsLimitedToLoopbackFixtures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	remoteProvider := Provider{ID: "remote-fixture", EndpointURL: server.URL}
	response, err := providerHTTPClientFor(remoteProvider).Get(server.URL)
	if err != nil {
		t.Fatalf("loopback httptest fixture request failed: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("fixture status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}

	for _, endpoint := range []string{
		"http://example.test:11434",
		"http://host.docker.internal:11434",
		"http://192.168.1.10:11434",
	} {
		provider := Provider{ID: "remote-fixture", EndpointURL: endpoint}
		if got := providerHTTPClientFor(provider); got != providerHTTPClient {
			t.Errorf("endpoint %q received test-local client; want production-restricted client", endpoint)
		}
	}
}

func TestProductionProviderHTTPClientStillRejectsLoopbackForRemoteProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("remote provider request reached loopback server")
	}))
	defer server.Close()

	dialer := &net.Dialer{Timeout: time.Second}
	client := newProviderHTTPClient(false)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("production provider client has unexpected transport")
	}
	transport.DialContext = providerPinnedDialContext(false, net.DefaultResolver, dialer.DialContext)
	defer transport.CloseIdleConnections()

	response, err := client.Get(server.URL)
	if response != nil {
		_ = response.Body.Close()
		t.Fatalf("response = %#v, want loopback rejection", response)
	}
	if err == nil || !strings.Contains(err.Error(), "remote provider endpoint cannot connect to a local host") {
		t.Fatalf("request error = %v, want local-host rejection", err)
	}
}

func newTestProviderTLSServer(t *testing.T) (*httptest.Server, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
	)
	if err != nil {
		t.Fatal(err)
	}
	parsedCert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsedCert)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	server.Config.TLSConfig = &tls.Config{Certificates: []tls.Certificate{certificate}}
	server.StartTLS()
	return server, roots
}
