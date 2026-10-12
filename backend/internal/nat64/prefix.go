// Package nat64 parses RFC 6052 Pref64 configuration and extracts embedded
// IPv4 destinations without performing network discovery.
package nat64

import (
	"errors"
	"net/netip"
	"sort"
	"strings"
)

const WellKnownPrefix = "64:ff9b::/96"

var ErrInvalidPrefixes = errors.New("NAT64 prefixes must be valid RFC 6052 IPv6 CIDRs")

var supportedPrefixLengths = map[int]struct{}{
	32: {},
	40: {},
	48: {},
	56: {},
	64: {},
	96: {},
}

// ParsePrefixes returns the RFC 6052 well-known prefix plus configured IPv6
// Pref64 values. Empty comma-separated fields are ignored; malformed,
// non-IPv6, and unsupported prefix lengths fail closed.
func ParsePrefixes(configured string) ([]netip.Prefix, error) {
	wellKnown, _ := netip.ParsePrefix(WellKnownPrefix)
	prefixes := []netip.Prefix{wellKnown}
	seen := map[netip.Prefix]struct{}{wellKnown: {}}

	for _, raw := range strings.Split(configured, ",") {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil || !prefix.Addr().Is6() || prefix.Addr().Is4In6() {
			return nil, ErrInvalidPrefixes
		}
		prefix = prefix.Masked()
		if _, ok := supportedPrefixLengths[prefix.Bits()]; !ok {
			return nil, ErrInvalidPrefixes
		}
		if _, exists := seen[prefix]; exists {
			continue
		}
		seen[prefix] = struct{}{}
		prefixes = append(prefixes, prefix)
	}
	sort.SliceStable(prefixes, func(i, j int) bool { return prefixes[i].Bits() > prefixes[j].Bits() })
	return prefixes, nil
}

// Extract returns an embedded IPv4 address, whether any supplied prefix
// matched, and whether that match used a valid RFC 6052 layout. A matched but
// malformed address must be rejected rather than treated as native IPv6.
func Extract(address netip.Addr, prefixes []netip.Prefix) (embedded netip.Addr, matched, valid bool) {
	if !address.IsValid() || !address.Is6() || address.Is4In6() {
		return netip.Addr{}, false, false
	}
	bytes := address.As16()
	for _, prefix := range prefixes {
		if !prefix.IsValid() || !prefix.Contains(address) {
			continue
		}
		var octets [4]byte
		var suffixStart int
		switch prefix.Bits() {
		case 32:
			copy(octets[:], bytes[4:8])
			if bytes[8] != 0 {
				return netip.Addr{}, true, false
			}
			suffixStart = 9
		case 40:
			copy(octets[:3], bytes[5:8])
			if bytes[8] != 0 {
				return netip.Addr{}, true, false
			}
			octets[3] = bytes[9]
			suffixStart = 10
		case 48:
			copy(octets[:2], bytes[6:8])
			if bytes[8] != 0 {
				return netip.Addr{}, true, false
			}
			copy(octets[2:], bytes[9:11])
			suffixStart = 11
		case 56:
			octets[0] = bytes[7]
			if bytes[8] != 0 {
				return netip.Addr{}, true, false
			}
			copy(octets[1:], bytes[9:12])
			suffixStart = 12
		case 64:
			if bytes[8] != 0 {
				return netip.Addr{}, true, false
			}
			copy(octets[:], bytes[9:13])
			suffixStart = 13
		case 96:
			copy(octets[:], bytes[12:16])
			suffixStart = 16
		default:
			return netip.Addr{}, true, false
		}
		for _, value := range bytes[suffixStart:] {
			if value != 0 {
				return netip.Addr{}, true, false
			}
		}
		return netip.AddrFrom4(octets), true, true
	}
	return netip.Addr{}, false, false
}
