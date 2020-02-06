package discovery

import (
	"fmt"
	"net"
)

var (
	// rfc1918Nets specifies the IPv4 private address blocks as defined by
	// by RFC1918 (10.0.0.0/8, 172.16.0.0/12, and 192.168.0.0/16).
	rfc1918Nets = []net.IPNet{
		ipNet("10.0.0.0", 8, 32),
		ipNet("172.16.0.0", 12, 32),
		ipNet("192.168.0.0", 16, 32),
	}

	// rfc2544Net specifies the the IPv4 block as defined by RFC2544
	// (198.18.0.0/15)
	rfc2544Net = ipNet("198.18.0.0", 15, 32)

	// rfc3849Net specifies the IPv6 documentation address block as defined
	// by RFC3849 (2001:DB8::/32).
	rfc3849Net = ipNet("2001:DB8::", 32, 128)

	// rfc3927Net specifies the IPv4 auto configuration address block as
	// defined by RFC3927 (169.254.0.0/16).
	rfc3927Net = ipNet("169.254.0.0", 16, 32)

	// rfc3964Net specifies the IPv6 to IPv4 encapsulation address block as
	// defined by RFC3964 (2002::/16).
	rfc3964Net = ipNet("2002::", 16, 128)

	// rfc4193Net specifies the IPv6 unique localNode address block as defined
	// by RFC4193 (FC00::/7).
	rfc4193Net = ipNet("FC00::", 7, 128)

	// rfc4380Net specifies the IPv6 teredo tunneling over UDP address block
	// as defined by RFC4380 (2001::/32).
	rfc4380Net = ipNet("2001::", 32, 128)

	// rfc4843Net specifies the IPv6 ORCHID address block as defined by
	// RFC4843 (2001:10::/28).
	rfc4843Net = ipNet("2001:10::", 28, 128)

	// rfc4862Net specifies the IPv6 stateless address autoconfiguration
	// address block as defined by RFC4862 (FE80::/64).
	rfc4862Net = ipNet("FE80::", 64, 128)

	// rfc5737Net specifies the IPv4 documentation address blocks as defined
	// by RFC5737 (192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24)
	rfc5737Net = []net.IPNet{
		ipNet("192.0.2.0", 24, 32),
		ipNet("198.51.100.0", 24, 32),
		ipNet("203.0.113.0", 24, 32),
	}

	// rfc6052Net specifies the IPv6 well-known prefix address block as
	// defined by RFC6052 (64:FF9B::/96).
	rfc6052Net = ipNet("64:FF9B::", 96, 128)

	// rfc6145Net specifies the IPv6 to IPv4 translated address range as
	// defined by RFC6145 (::FFFF:0:0:0/96).
	rfc6145Net = ipNet("::FFFF:0:0:0", 96, 128)

	// rfc6598Net specifies the IPv4 block as defined by RFC6598 (100.64.0.0/10)
	rfc6598Net = ipNet("100.64.0.0", 10, 32)

	// onionCatNet defines the IPv6 address block used to support Tor.
	// bitcoind encodes a .onion address as a 16 byte number by decoding the
	// address prior to the .onion (i.e. the key hash) base32 into a ten
	// byte number. It then stores the first 6 bytes of the address as
	// 0xfd, 0x87, 0xd8, 0x7e, 0xeb, 0x43.
	//
	// This is the same range used by OnionCat, which is part part of the
	// RFC4193 unique localNode IPv6 range.
	//
	// In summary the format is:
	// { magic 6 bytes, 10 bytes base32 decode of key hash }
	onionCatNet = ipNet("fd87:d87e:eb43::", 48, 128)

	// zero4Net defines the IPv4 address block for address staring with 0
	// (0.0.0.0/8).
	zero4Net = ipNet("0.0.0.0", 8, 32)

	// heNet defines the Hurricane Electric IPv6 address block.
	heNet = ipNet("2001:470::", 32, 128)
)

// ipNet returns a net.IPNet struct given the passed IP address string, number
// of one bits to include at the start of the mask, and the total number of bits
// for the mask.
func ipNet(ip string, ones, bits int) net.IPNet {
	return net.IPNet{IP: net.ParseIP(ip), Mask: net.CIDRMask(ones, bits)}
}

// IsIPv4 returns whether or not the given address is an IPv4 address.
func IsIPv4(na net.IP) bool {
	return na.To4() != nil
}

// IsLocal returns whether or not the given address is a localNode address.
func IsLocal(na net.IP) bool {
	return na.IsLoopback() || zero4Net.Contains(na)
}

// IsOnionCatTor returns whether or not the passed address is in the IPv6 range
// used by bitcoin to support Tor (fd87:d87e:eb43::/48).  Note that this range
// is the same range used by OnionCat, which is part of the RFC4193 unique localNode
// IPv6 range.
func IsOnionCatTor(na net.IP) bool {
	return onionCatNet.Contains(na)
}

// IsRFC1918 returns whether or not the passed address is part of the IPv4
// private network address space as defined by RFC1918 (10.0.0.0/8,
// 172.16.0.0/12, or 192.168.0.0/16).
func IsRFC1918(na net.IP) bool {
	for _, rfc := range rfc1918Nets {
		if rfc.Contains(na) {
			return true
		}
	}
	return false
}

// IsRFC2544 returns whether or not the passed address is part of the IPv4
// address space as defined by RFC2544 (198.18.0.0/15)
func IsRFC2544(na net.IP) bool {
	return rfc2544Net.Contains(na)
}

// IsRFC3849 returns whether or not the passed address is part of the IPv6
// documentation range as defined by RFC3849 (2001:DB8::/32).
func IsRFC3849(na net.IP) bool {
	return rfc3849Net.Contains(na)
}

// IsRFC3927 returns whether or not the passed address is part of the IPv4
// autoconfiguration range as defined by RFC3927 (169.254.0.0/16).
func IsRFC3927(na net.IP) bool {
	return rfc3927Net.Contains(na)
}

// IsRFC3964 returns whether or not the passed address is part of the IPv6 to
// IPv4 encapsulation range as defined by RFC3964 (2002::/16).
func IsRFC3964(na net.IP) bool {
	return rfc3964Net.Contains(na)
}

// IsRFC4193 returns whether or not the passed address is part of the IPv6
// unique localNode range as defined by RFC4193 (FC00::/7).
func IsRFC4193(na net.IP) bool {
	return rfc4193Net.Contains(na)
}

// IsRFC4380 returns whether or not the passed address is part of the IPv6
// teredo tunneling over UDP range as defined by RFC4380 (2001::/32).
func IsRFC4380(na net.IP) bool {
	return rfc4380Net.Contains(na)
}

// IsRFC4843 returns whether or not the passed address is part of the IPv6
// ORCHID range as defined by RFC4843 (2001:10::/28).
func IsRFC4843(na net.IP) bool {
	return rfc4843Net.Contains(na)
}

// IsRFC4862 returns whether or not the passed address is part of the IPv6
// stateless address autoconfiguration range as defined by RFC4862 (FE80::/64).
func IsRFC4862(na net.IP) bool {
	return rfc4862Net.Contains(na)
}

// IsRFC5737 returns whether or not the passed address is part of the IPv4
// documentation address space as defined by RFC5737 (192.0.2.0/24,
// 198.51.100.0/24, 203.0.113.0/24)
func IsRFC5737(na net.IP) bool {
	for _, rfc := range rfc5737Net {
		if rfc.Contains(na) {
			return true
		}
	}

	return false
}

// IsRFC6052 returns whether or not the passed address is part of the IPv6
// well-known prefix range as defined by RFC6052 (64:FF9B::/96).
func IsRFC6052(na net.IP) bool {
	return rfc6052Net.Contains(na)
}

// IsRFC6145 returns whether or not the passed address is part of the IPv6 to
// IPv4 translated address range as defined by RFC6145 (::FFFF:0:0:0/96).
func IsRFC6145(na net.IP) bool {
	return rfc6145Net.Contains(na)
}

// IsRFC6598 returns whether or not the passed address is part of the IPv4
// shared address space specified by RFC6598 (100.64.0.0/10)
func IsRFC6598(na net.IP) bool {
	return rfc6598Net.Contains(na)
}

// IsValid returns whether or not the passed address is valid.  The address is
// considered invalid under the following circumstances:
// IPv4: It is either a zero or all bits set address.
// IPv6: It is either a zero or RFC3849 documentation address.
func IsValid(na net.IP) bool {
	// IsUnspecified returns if address is 0, so only all bits set, and
	// RFC3849 need to be explicitly checked.
	return na != nil && !(na.IsUnspecified() ||
		na.Equal(net.IPv4bcast))
}

// IsRoutable returns whether or not the passed address is routable over
// the public internet.  This is true as long as the address is valid and is not
// in any reserved ranges.
func IsRoutable(na net.IP) bool {
	return IsValid(na) && !(IsRFC1918(na) || IsRFC2544(na) ||
		IsRFC3927(na) || IsRFC4862(na) || IsRFC3849(na) ||
		IsRFC4843(na) || IsRFC5737(na) || IsRFC6598(na) ||
		IsLocal(na) || (IsRFC4193(na) && !IsOnionCatTor(na)))
}

// GroupKey returns a string representing the network group an address is part
// of.  This is the /16 for IPv4, the /32 (/36 for he.net) for IPv6, the string
// "localNode" for a localNode address, the string "tor:key" where key is the /4 of the
// onion address for Tor address, and the string "unroutable" for an unroutable
// address.
func GroupKey(na net.IP) string {
	if IsLocal(na) {
		return "localNode"
	}
	if !IsRoutable(na) {
		return "unroutable"
	}
	if IsIPv4(na) {
		return na.Mask(net.CIDRMask(16, 32)).String()
	}
	if IsRFC6145(na) || IsRFC6052(na) {
		// last four bytes are the ip address
		ip := na[12:16]
		return ip.Mask(net.CIDRMask(16, 32)).String()
	}

	if IsRFC3964(na) {
		ip := na[2:6]
		return ip.Mask(net.CIDRMask(16, 32)).String()

	}
	if IsRFC4380(na) {
		// teredo tunnels have the last 4 bytes as the v4 address XOR
		// 0xff.
		ip := net.IP(make([]byte, 4))
		for i, byte := range na[12:16] {
			ip[i] = byte ^ 0xff
		}
		return ip.Mask(net.CIDRMask(16, 32)).String()
	}
	if IsOnionCatTor(na) {
		// group is keyed off the first 4 bits of the actual onion key.
		return fmt.Sprintf("tor:%d", na[6]&((1<<4)-1))
	}

	// OK, so now we know ourselves to be a IPv6 address.
	// bitcoind uses /32 for everything, except for Hurricane Electric's
	// (he.net) IP range, which it uses /36 for.
	bits := 32
	if heNet.Contains(na) {
		bits = 36
	}

	return na.Mask(net.CIDRMask(bits, 128)).String()
}
