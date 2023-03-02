package peerexchange

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/book"
)

// crawler is used to crawl reachable peers and query them for addresses.
type crawler struct {
	logger log.Log
	host   host.Host

	book *book.Book
	disc *peerExchange
}

func newCrawler(logger log.Log, h host.Host, book *book.Book, disc *peerExchange) *crawler {
	return &crawler{
		logger: logger,
		host:   h,
		disc:   disc,
		book:   book,
	}
}

// Crawl connects to number of peers, limit by concurrent parameter, and learns
// new peers from it.
func (r *crawler) Crawl(ctx context.Context, concurrent int) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs := r.book.DrainQueue(concurrent)
	if len(addrs) == 0 {
		return errors.New("can't connect to the network without addresses")
	}
	var eg errgroup.Group
	for _, addr := range addrs {
		src, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			return fmt.Errorf("can't parse %v: %w", addr, err)
		}
		if src.ID == r.host.ID() {
			continue
		}
		eg.Go(func() error {
			peers, err := getPeers(ctx, r.host, r.disc, src)
			if err != nil {
				r.logger.With().Debug("failed to get more peers",
					log.Stringer("peer", src),
					log.Err(err),
				)
				r.book.Update(src.ID.String(), book.Fail, book.Disconnected)
				r.host.Peerstore().ClearAddrs(src.ID)
				r.host.Network().ClosePeer(src.ID)
			} else {
				r.book.Update(src.ID.String(), book.Success, book.Connected)
				for _, addr := range peers {
					r.book.Add(src.ID.String(), addr.ID.String(), addr.Addr)
				}
			}
			return nil
		})
	}
	return eg.Wait()
}

type maddrIdTuple struct {
	ID   peer.ID
	Addr ma.Multiaddr
}

func getPeers(ctx context.Context, h host.Host, disc *peerExchange, peer *peer.AddrInfo) ([]maddrIdTuple, error) {
	err := h.Connect(ctx, *peer)
	if err != nil {
		return nil, err
	}
	res, err := disc.Request(ctx, peer.ID)
	if err != nil {
		return nil, err
	}
	rst := make([]maddrIdTuple, 0, len(res))
	for _, raw := range res {
		maddr, id, err := parseIdMaddr(raw)
		if err != nil {
			return nil, err
		}
		if id == h.ID() {
			return nil, fmt.Errorf("malformed reply: included our own id: %s", h.ID())
		}
		rst = append(rst, maddrIdTuple{id, maddr})
	}
	return rst, nil
}
