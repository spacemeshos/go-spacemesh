package types

import "net"

// Use this struct to extend Addr interface in the golang stdlib
type IPAddr struct {
	Host    string
	Port    string
	NetworkType    string
}

// Implements the String() method of Addr interface
func (a IPAddr) String() string {
	if net.ParseIP(a.Host) == nil {
		return "not a valid textual representation of an IP address"
	}
	return net.JoinHostPort(a.Host, a.Port)
}

// Implements the Network() method of Addr interface
func (a IPAddr) Network() string {
	return a.NetworkType
}
