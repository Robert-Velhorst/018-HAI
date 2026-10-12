package nat64

import (
	"net/netip"
	"testing"
)

func TestParsePrefixes(t *testing.T) {
	prefixes, err := ParsePrefixes(" 64:ff9b::/64,2001:db8:1234::/48,2001:db8:1234::/48,, 2001:db8:abcd::/64 ")
	if err != nil {
		t.Fatal(err)
	}
	if len(prefixes) != 4 {
		t.Fatalf("got %d prefixes, want well-known plus three unique configured prefixes: %v", len(prefixes), prefixes)
	}
	if prefixes[0].String() != WellKnownPrefix || prefixes[1].String() != "64:ff9b::/64" {
		t.Fatalf("prefixes are not ordered most-specific-first: %v", prefixes)
	}
}

func TestParsePrefixesRejectsInvalidConfiguration(t *testing.T) {
	for _, value := range []string{"not-a-cidr", "10.0.0.0/8", "2001:db8::/33", "2001:db8::/128", "::ffff:192.0.2.0/120"} {
		t.Run(value, func(t *testing.T) {
			if _, err := ParsePrefixes(value); err == nil {
				t.Fatalf("ParsePrefixes(%q) succeeded", value)
			}
		})
	}
}

func TestExtractRFC6052Layouts(t *testing.T) {
	tests := []struct {
		prefix string
		addr   string
	}{
		{"2001:db8::/32", "2001:db8:c000:221::"},
		{"2001:db8:1000::/40", "2001:db8:10c0:2:21::"},
		{"2001:db8:1234::/48", "2001:db8:1234:c000:2:2100::"},
		{"2001:db8:122:300::/56", "2001:db8:122:3c0:0:221::"},
		{"2001:db8:122:344::/64", "2001:db8:122:344:c0:2:2100::"},
		{WellKnownPrefix, "64:ff9b::c000:221"},
	}
	for _, tt := range tests {
		t.Run(tt.prefix, func(t *testing.T) {
			prefix, err := netip.ParsePrefix(tt.prefix)
			if err != nil {
				t.Fatal(err)
			}
			address := netip.MustParseAddr(tt.addr)
			got, matched, valid := Extract(address, []netip.Prefix{prefix})
			if !matched || !valid || got != netip.MustParseAddr("192.0.2.33") {
				t.Fatalf("Extract(%s) = %s, matched=%v valid=%v", address, got, matched, valid)
			}
		})
	}
}

func TestExtractDistinguishesNativeAndMalformedPrefixedIPv6(t *testing.T) {
	prefix, _ := netip.ParsePrefix("2001:db8:1234::/48")
	if _, matched, valid := Extract(netip.MustParseAddr("2001:db8:ffff::1"), []netip.Prefix{prefix}); matched || valid {
		t.Fatal("native IPv6 outside Pref64 was treated as translated")
	}
	for _, address := range []string{
		"2001:db8:1234:c000:2:2100:1::", // non-zero suffix
		"2001:db8:1234:c000:102:2100::", // non-zero u octet
	} {
		addr := netip.MustParseAddr(address)
		if _, matched, valid := Extract(addr, []netip.Prefix{prefix}); !matched || valid {
			t.Fatalf("malformed Pref64 address %s: matched=%v valid=%v", addr, matched, valid)
		}
	}
}
