package net

import "net"

type Address struct {
	host string
	port string
	network string
}

// Implements the String() method of Addr interface in the golang stdlib
func (a Address) String() string {
	if net.ParseIP(a.host) == nil {
		return "not a valid textual representation of an IP address"
	}
	return net.JoinHostPort(a.host, a.port)
}

// Implements the Network() method of Addr interface in the golang stdlib
func (a Address) Network() string {
	return a.network
}