package peerexchange

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"go.uber.org/atomic"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	protocolName = "/peerexchange/v1.0.0"
	// messageTimeout is the timeout for the whole stream lifetime.
	messageTimeout = 10 * time.Second
)

type peerExchange struct {
	listen *atomic.Uint32

	h      host.Host
	book   *addrBook
	logger log.Log
}

// newPeerExchange is a constructor for a protocol protocol provider.
func newPeerExchange(h host.Host, rt *addrBook, listen uint16, log log.Log) *peerExchange {
	ga := &peerExchange{
		h:      h,
		book:   rt,
		logger: log,
		listen: atomic.NewUint32(uint32(listen)),
	}
	h.SetStreamHandler(protocolName, ga.handler)
	return ga
}

func (p *peerExchange) handler(stream network.Stream) {
	defer stream.Close()
	t := time.Now()
	logger := p.logger.WithFields(log.String("protocol", protocolName),
		log.String("from", stream.Conn().RemotePeer().Pretty())).With()

	var port uint16
	if _, err := codec.DecodeFrom(stream, &port); err != nil {
		logger.Warning("failed to decode request into address", log.Err(err))
		return
	}
	logger.Debug("got request", log.Uint16("listen-port", port))
	if port != 0 {
		ip, err := manet.ToIP(stream.Conn().RemoteMultiaddr())
		if err != nil {
			logger.Warning("failed to recover ip from the connection", log.Err(err))
			return
		}
		ma, err := manet.FromNetAddr(&net.TCPAddr{
			IP:   ip,
			Port: int(port),
		})
		if err != nil {
			logger.Error("failed to parse netaddr", log.Err(err))
			return
		}
		raw := fmt.Sprintf("%s/p2p/%s", ma, stream.Conn().RemotePeer())
		info, err := parseAddrInfo(raw)
		if err != nil {
			logger.Error("failed to parse created address", log.String("address", raw), log.Err(err))
			return
		}
		p.book.AddAddress(info, info)
	}
	results := p.book.AddressCache()

	for i := range results {
		if results[i].ID == stream.Conn().RemotePeer() {
			results[i] = results[len(results)-1]
			results = results[:len(results)-1]
			break
		}
	}
	response := make([]string, 0, len(results))
	for _, addr := range results {
		response = append(response, addr.RawAddr)
	}
	// todo: limit results to message size
	_ = stream.SetDeadline(time.Now().Add(messageTimeout))
	defer stream.SetDeadline(time.Time{})
	wr := bufio.NewWriter(stream)
	_, err := codec.EncodeTo(wr, response)
	if err == nil {
		err = wr.Flush()
	}
	if err != nil {
		logger.Warning("failed to write response", log.Err(err))
		return
	}
	logger.Debug("response is sent",
		log.Int("size", len(results)),
		log.Duration("time_to_make", time.Since(t)))
}

// UpdateExternalPort updates port that is used for advertisement.
func (p *peerExchange) UpdateExternalPort(port uint16) {
	p.listen.Store(uint32(port))
}

// ExternalPort returns currently configured external port.
func (p *peerExchange) ExternalPort() uint16 {
	return uint16(p.listen.Load())
}

// Request addresses from a remote node, it will block and return the results returned from the node.
func (p *peerExchange) Request(ctx context.Context, pid peer.ID) ([]*addrInfo, error) {
	logger := p.logger.WithContext(ctx).WithFields(
		log.String("type", "getaddresses"),
		log.String("to", pid.String())).With()
	logger.Debug("sending request")

	stream, err := p.h.NewStream(network.WithNoDial(ctx, "existing"), pid, protocolName)
	if err != nil {
		return nil, fmt.Errorf("failed to create a discovery stream: %w", err)
	}
	defer stream.Close()

	_ = stream.SetDeadline(time.Now().Add(messageTimeout))
	defer stream.SetDeadline(time.Time{})
	if _, err := codec.EncodeTo(stream, p.ExternalPort()); err != nil {
		return nil, fmt.Errorf("failed to send GetAddress request: %w", err)
	}

	addrs := []string{}
	if _, err := codec.DecodeFrom(bufio.NewReader(stream), &addrs); err != nil {
		return nil, fmt.Errorf("failed to read addresses in response: %w", err)
	}

	infos := make([]*addrInfo, 0, len(addrs))
	for _, raw := range addrs {
		info, err := parseAddrInfo(raw)
		if err != nil {
			return nil, fmt.Errorf("failed parsing: %w", err)
		}
		if info.ID == p.h.ID() {
			return nil, fmt.Errorf("peer shouldn't reply with address for our id")
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// parseAddrInfo parses required info from string in multiaddr format.
func parseAddrInfo(raw string) (*addrInfo, error) {
	addr, err := ma.NewMultiaddr(raw)
	if err != nil {
		return nil, fmt.Errorf("received invalid address %v: %w", raw, err)
	}
	_, pid := peer.SplitAddr(addr)
	if len(pid) == 0 {
		return nil, fmt.Errorf("address without peer id %v", raw)
	}
	if strings.HasPrefix(raw, "/dns") {
		return &addrInfo{ID: pid, RawAddr: raw, addr: addr}, nil
	}
	ip, err := manet.ToIP(addr)
	if err != nil {
		return nil, fmt.Errorf("address without ip %v: %w", raw, err)
	}
	return &addrInfo{ID: pid, IP: ip, RawAddr: raw, addr: addr}, nil
}

// addrInfo stores relevant information for discovery.
type addrInfo struct {
	IP      net.IP
	ID      peer.ID
	RawAddr string
	addr    ma.Multiaddr
}

func (a *addrInfo) String() string {
	return a.RawAddr
}

// Addr returns pointeer to multiaddr.
func (a *addrInfo) Addr() ma.Multiaddr {
	return a.addr
}
