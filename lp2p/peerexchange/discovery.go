package peerexchange

import (
	"context"
	"fmt"
	"net"

	"github.com/libp2p/go-eventbus"
	"github.com/libp2p/go-libp2p-core/event"
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
	sub, err := h.EventBus().Subscribe(new(event.EvtLocalAddressesUpdated), eventbus.BufSize(4))
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to eventbus: %w", err)
	}
	d.eg.Go(func() error {
		defer sub.Close()
		tryBest := func() {
			best, err := BestHostAddress(h)
			if err != nil {
				logger.With().Warning("failed to select best address from host", log.Err(err))
				return
			}
			if IsRoutable(best.IP) {
				d.book.AddAddress(best, best)
			}
		}
		tryBest()
		for {
			select {
			case <-sub.Out():
				tryBest()
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})
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

	best, err := BestHostAddress(h)
	if err != nil {
		return nil, err
	}
	d.book.AddAddresses(bootnodes, best)

	d.crawl = newCrawler(h, d.book, newPeerExchange(h, d.book, logger), logger)
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

// BestHostAddress returns routable address if exists, otherwise it returns first available address.
func BestHostAddress(h host.Host) (*addrInfo, error) {
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
