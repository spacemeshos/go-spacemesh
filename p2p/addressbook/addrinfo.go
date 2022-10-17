package addressbook

import (
	"fmt"
	"net"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// AddrInfo stores relevant information for discovery.
type AddrInfo struct {
	IP      net.IP
	ID      peer.ID
	RawAddr string
	addr    ma.Multiaddr
}

func (a *AddrInfo) String() string {
	return a.RawAddr
}

// Addr returns pointer to multiaddr.
func (a *AddrInfo) Addr() ma.Multiaddr {
	return a.addr
}

// SetAddr sets the multiaddr for the AddrInfo.
func (a *AddrInfo) SetAddr(addr ma.Multiaddr) {
	a.addr = addr
}

// ParseAddrInfo parses required info from string in multiaddr format.
func ParseAddrInfo(raw string) (*AddrInfo, error) {
	addr, err := ma.NewMultiaddr(raw)
	if err != nil {
		return nil, fmt.Errorf("received invalid address %v: %w", raw, err)
	}
	_, pid := peer.SplitAddr(addr)
	if len(pid) == 0 {
		return nil, fmt.Errorf("address without peer id %v", raw)
	}
	if IsDNSAddress(raw) {
		return &AddrInfo{ID: pid, RawAddr: raw, addr: addr}, nil
	}
	ip, err := manet.ToIP(addr)
	if err != nil {
		return nil, fmt.Errorf("address without ip %v: %w", raw, err)
	}
	return &AddrInfo{ID: pid, IP: ip, RawAddr: raw, addr: addr}, nil
}
