package peerexchange

import (
	"context"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"

	"github.com/spacemeshos/go-spacemesh/log"
)

const maxConcurrentRequests = 3

type queryResult struct {
	Src    *addrInfo
	Result []*addrInfo
	Err    error
}

// crawler is used to crawl reachable peers and query them for addresses.
type crawler struct {
	logger log.Log
	host   host.Host

	book *addrBook
	disc *peerExchange
}

func newCrawler(h host.Host, book *addrBook, disc *peerExchange, logger log.Log) *crawler {
	return &crawler{
		logger: logger,
		host:   h,
		book:   book,
		disc:   disc,
	}
}

// Bootstrap crawls the network until context is canceled or all reachable peers are crawled.
func (r *crawler) Bootstrap(ctx context.Context) error {
	seen := map[peer.ID]struct{}{r.host.ID(): {}}
	servers := r.book.AddressCache()
	for _, srv := range servers {
		seen[srv.ID] = struct{}{}
	}

	r.logger.With().Debug("starting crawl", log.Int("servers", len(servers)))
	for {
		if len(servers) == 0 {
			r.logger.Debug("crawl finished; no more servers to query")
			return nil
		}
		result, err := r.query(ctx, servers)
		if err != nil {
			r.logger.Debug("crawl finished by timeout")
			return err
		}
		var round []*addrInfo
		for _, addr := range result {
			if _, exist := seen[addr.ID]; !exist {
				seen[addr.ID] = struct{}{}
				round = append(round, addr)
			}
		}
		servers = round
	}
}

func (r *crawler) query(ctx context.Context, servers []*addrInfo) ([]*addrInfo, error) {
	var (
		out        []*addrInfo
		seen       = map[peer.ID]struct{}{}
		reschan    = make(chan queryResult)
		pending, i int
	)
	for {
		for ; i < len(servers) && pending < maxConcurrentRequests; i++ {
			addr := servers[i]
			if addr.ID == r.host.ID() {
				continue
			}
			pending++

			go func() {
				ainfo, err := peer.AddrInfoFromP2pAddr(addr.Addr())
				if err == nil {
					err = r.host.Connect(ctx, *ainfo)
				}
				var res []*addrInfo
				if err == nil {
					res, err = r.disc.Request(ctx, addr.ID)
				}
				select {
				case reschan <- queryResult{Src: addr, Result: res, Err: err}:
				case <-ctx.Done():
				}
			}()
		}
		if pending == 0 {
			return out, nil
		}

		select {
		case cr := <-reschan:
			r.book.Attempt(cr.Src.ID)
			pending--
			if cr.Err != nil {
				// TODO(dshulyak) also remove peer from persistent addrbook on validation error
				r.host.Peerstore().ClearAddrs(cr.Src.ID)
				// the address may actually be outdate, since we don't actively update address book
				r.logger.With().Debug("peer failed to respond to protocol queries",
					log.String("peer", cr.Src.ID.String()),
					log.Err(cr.Err))
				continue
			}
			// TODO(dshulyak) will be correct to call after EventHandshakeComplete is received
			r.book.Good(cr.Src.ID)
			for _, a := range cr.Result {
				if _, ok := seen[a.ID]; ok {
					continue
				}
				seen[a.ID] = struct{}{}
				out = append(out, a)
				r.book.AddAddress(a, cr.Src)
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
