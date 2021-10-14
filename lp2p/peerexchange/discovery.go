package peerexchange

import (
	"context"
	"fmt"
	"math"
	"net"
	"strconv"

	"github.com/libp2p/go-libp2p-core/host"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/log"
)

// Config for Discovery.
type Config struct {
	BootstrapNodes []string
	DataDir        string
}

// Discovery is struct that holds the protocol components, the protocol definition, the addr book data structure and more.
type Discovery struct {
	logger log.Log

	cancel context.CancelFunc
	eg     errgroup.Group

	book  *addrBook
	crawl *crawler
}

// New creates a Discovery instance.
func New(logger log.Log, h host.Host, config Config) (*Discovery, error) {
	ctx, cancel := context.WithCancel(context.Background())
	d := &Discovery{
		logger: logger,
		cancel: cancel,
		book:   newAddrBook(config, logger),
	}
	d.eg.Go(func() error {
		d.book.Persist(ctx)
		return ctx.Err()
	})

	bootnodes := make([]*addrInfo, 0, len(config.BootstrapNodes))
	for _, raw := range config.BootstrapNodes {
		info, err := parseAddrInfo(raw)
		if err != nil {
			return nil, fmt.Errorf("failed to parse bootstrap node: %w", err)
		}
		bootnodes = append(bootnodes, info)
	}

	best, err := bestHostAddress(h)
	if err != nil {
		return nil, err
	}
	d.book.AddAddresses(bootnodes, best)
	port, err := portFromAddress(h)
	if err != nil {
		return nil, err
	}
	d.crawl = newCrawler(h, d.book, newPeerExchange(h, d.book, port, logger), logger)
	return d, nil
}

// Stop stops the discovery service
func (d *Discovery) Stop() {
	d.cancel()
	d.eg.Wait()
}

// Bootstrap runs a refresh and tries to get a minimum number of nodes in the addrBook.
func (d *Discovery) Bootstrap(ctx context.Context) error {
	return d.crawl.Bootstrap(ctx)
}

// bestHostAddress returns routable address if exists, otherwise it returns first available address.
func bestHostAddress(h host.Host) (*addrInfo, error) {
	var (
		infos = make([]*addrInfo, 0, len(h.Addrs()))
		best  *addrInfo
	)
	for _, addr := range h.Addrs() {
		ip, err := manet.ToIP(addr)
		if err != nil {
			return nil, fmt.Errorf("failed to recover ip from host address %v: %w", addr, err)
		}
		full := ma.Join(addr, ma.StringCast("/p2p/"+h.ID().String()))
		info := &addrInfo{
			IP:      ip,
			ID:      h.ID(),
			RawAddr: full.String(),
			addr:    full,
		}
		routable := IsRoutable(ip)
		if routable {
			best = info
			break
		}
		infos = append(infos, info)
	}
	if best == nil {
		if len(infos) == 0 {
			best = &addrInfo{
				IP: net.IP{127, 0, 0, 1},
				ID: h.ID(),
			}
		} else {
			best = infos[0]
		}
	}
	return best, nil
}

func portFromAddress(h host.Host) (uint16, error) {
	for _, addr := range h.Addrs() {
		netaddr, err := manet.ToNetAddr(addr)
		if err != nil {
			return 0, fmt.Errorf("failed to case addr %s to netaddr: %w", addr, err)
		}
		_, portStr, err := net.SplitHostPort(netaddr.String())
		if err != nil {
			return 0, fmt.Errorf("parsed netaddr %s is not in expected format: %w", netaddr, err)
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return 0, fmt.Errorf("failed to convert port %s to int", portStr)
		}
		if int(uint16(port)) < port {
			return 0, fmt.Errorf("port %d cant fit into uint16 %d", port, math.MaxUint16)
		}
		return uint16(port), nil
	}
	return 0, nil
}
