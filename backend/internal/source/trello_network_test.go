package source

import (
	"net"
	"net/netip"
	"net/url"
	"testing"
)

func TestTrelloResolvedAddressesRejectUnsafeRangesDespiteDevelopmentOverride(t *testing.T) {
	t.Setenv("CONNECTED_SOURCE_HTTP_ALLOW_LINK_LOCAL", "true")

	unsafe := []string{
		"0.0.0.0", "127.0.0.1", "10.1.2.3", "172.16.1.2", "192.168.1.2",
		"169.254.169.254", "169.254.1.2", "100.64.0.1", "192.0.2.1", "192.88.99.1",
		"198.18.0.1", "198.51.100.1", "203.0.113.1", "224.0.0.1",
		"240.0.0.1", "::", "::1", "fe80::1", "fc00::1", "ff02::1",
		"::ffff:169.254.169.254", "2001:db8::1",
	}
	for _, value := range unsafe {
		t.Run(value, func(t *testing.T) {
			ip, err := netip.ParseAddr(value)
			if err != nil {
				t.Fatalf("test address %q is invalid: %v", value, err)
			}
			if trelloPublicAddress(ip) {
				t.Fatalf("Trello accepted non-public address %q", value)
			}
			if _, err := trelloPublicResolvedAddress([]net.IPAddr{{IP: net.ParseIP(value)}}); err == nil {
				t.Fatalf("resolved-address validation accepted %q", value)
			}
		})
	}

	for _, value := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		t.Run("public-"+value, func(t *testing.T) {
			ip, err := netip.ParseAddr(value)
			if err != nil || !trelloPublicAddress(ip) {
				t.Fatalf("Trello rejected public address %q", value)
			}
			got, err := trelloPublicResolvedAddress([]net.IPAddr{{IP: net.ParseIP(value)}})
			if err != nil || got != ip {
				t.Fatalf("resolved-address validation = %v, %v; want %v", got, err, ip)
			}
		})
	}
}

func TestTrelloResolvedAddressValidationRejectsMixedDNSAnswers(t *testing.T) {
	answers := []net.IPAddr{
		{IP: net.ParseIP("1.1.1.1")},
		{IP: net.ParseIP("169.254.169.254")},
	}
	if _, err := trelloPublicResolvedAddress(answers); err == nil {
		t.Fatal("mixed public and metadata DNS answers were accepted")
	}
}

func TestTrelloTransportUsesSharedPolicyOnlyForExplicitLoopback(t *testing.T) {
	t.Setenv("CONNECTED_SOURCE_HTTP_LOOPBACK_ADDRS", "127.0.0.1:8080")
	loopback, _ := url.Parse("http://127.0.0.1:8080")
	if got, want := trelloHTTPTransport(loopback), sourceHTTPTransport(); got != want {
		t.Fatal("loopback fixture should retain shared local transport")
	}

	remote, _ := url.Parse("https://api.trello.com")
	if got, want := trelloHTTPTransport(remote), sourceHTTPTransport(); got == want {
		t.Fatal("remote Trello endpoint reused the override-controlled source transport")
	}
}
