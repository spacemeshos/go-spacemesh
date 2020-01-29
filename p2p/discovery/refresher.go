package discovery

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"net"
	"time"
)

// XXX TODO: move this impl to the upper protocol struct

const (

	// minTimeBetweenQueries is a minimum time between attempts to query a peer.
	minTimeBetweenQueries = 500 * time.Millisecond
	// lastQueriesCacheSize is the maximum size of the query cache map
	lastQueriesCacheSize = 100

	maxConcurrentRequests = 3
)

// ErrBootAbort is returned when when bootstrap is canceled by context cancel
var ErrBootAbort = errors.New("bootstrap canceled by signal")

// refresher is used to bootstrap and requestAddresses peers in the addrbook
type refresher struct {
	logger       log.Log
	localAddress *node.NodeInfo

	book      addressBook
	bootNodes []*node.NodeInfo

	disc        Protocol
	lastQueries map[p2pcrypto.PublicKey]time.Time

	quit chan struct{}
}

func newRefresher(local p2pcrypto.PublicKey, book addressBook, disc Protocol, bootnodes []*node.NodeInfo, logger log.Log) *refresher {
	//todo: trigger requestAddresses every X with random nodes
	return &refresher{
		logger:       logger,
		localAddress: &node.NodeInfo{ID: local.Array(), IP: net.IPv4zero},
		book:         book,
		disc:         disc,
		bootNodes:    bootnodes,
		lastQueries:  make(map[p2pcrypto.PublicKey]time.Time),
	}
}

func (r *refresher) Bootstrap(ctx context.Context, numpeers int) error {
	var err error
	tries := 0
	servers := make([]*node.NodeInfo, 0, len(r.bootNodes))

	// The following loop will add the pre-set bootstrap nodes and query them for results
	// if there were any results but we didn't reach numpeers it will try the same procedure
	// using the results as servers to query. every failed loop will wait a backoff
	// to let other nodes populate before flooding with queries.
	size := r.book.NumAddresses()
	if size == 0 {
		r.book.AddAddresses(r.bootNodes, r.localAddress)
		size = len(r.bootNodes)
		defer func() {
			// currently we only have  the discovery address of bootnodes in the configuration so let them pick their own neighbors.
			for _, b := range r.bootNodes {
				r.book.RemoveAddress(b.PublicKey())
			}
		}()
	}

	r.logger.Info("Bootstrap: starting with %v sized table", size)

loop:
	for {
		srv := r.book.AddressCache() // get fresh members to query
		servers = srv[:util.Min(numpeers, len(srv))]
		res := r.requestAddresses(ctx, servers)
		tries++
		r.logger.Info("Bootstrap : %d try gave %v results", tries, len(res))

		newsize := r.book.NumAddresses()
		wanted := numpeers

		if newsize-size >= wanted {
			r.logger.Info("Stopping bootstrap, achieved %v need %v", newsize-size, wanted)
			break
		}

		timer := time.NewTimer(time.Duration(tries) * time.Second) // BACKOFF

		//todo: stop refreshes with context
		select {
		case <-ctx.Done():
			err = ErrBootAbort
			break loop
		case <-timer.C:
			continue
		}
	}

	return err
}

func expire(m map[p2pcrypto.PublicKey]time.Time) {
	t := time.Now()
	c := 0
	for k, v := range m {
		if t.Sub(v) > minTimeBetweenQueries {
			delete(m, k)
			return
		}
		c++ // is better than go ?
		if c >= lastQueriesCacheSize {
			// delete last randomly selected key if none meet requirements
			//todo: use slice to keep order? this might result with quite new peer deleted
			delete(m, k)
			return
		}
	}
}

type queryResult struct {
	src *node.NodeInfo
	res []*node.NodeInfo
	err error
}

// pingThenGetAddresses is sending a ping, then find node, then return results on given chan.
func pingThenGetAddresses(p Protocol, addr *node.NodeInfo, qr chan queryResult) {
	// TODO: check whether we pinged recently and maybe skip pinging
	err := p.Ping(addr.PublicKey())

	if err != nil {
		qr <- queryResult{src: addr, err: err}
		return
	}
	res, err := p.GetAddresses(addr.PublicKey())

	if err != nil {
		qr <- queryResult{src: addr, err: err}
		return
	}
	qr <- queryResult{addr, res, nil}
}

// requestAddresses will crawl the network looking for new peer addresses.
func (r *refresher) requestAddresses(ctx context.Context, servers []*node.NodeInfo) []*node.NodeInfo {

	// todo: here we stop only after we've tried querying or queried all addrs
	// 	maybe we should stop after we've reached a certain amount ? (needMoreAddresses..)
	// todo: revisit this area to think about if lastQueries is even needed, since we're going
	// 		to probably sleep for more than minTimeBetweenQueries between running requestAddresses
	//		calls. maybe we can use only the seen cache instead.
	var out []*node.NodeInfo

	seen := make(map[p2pcrypto.PublicKey]struct{})
	seen[r.localAddress.PublicKey()] = struct{}{}

	now := time.Now()
	pending := 0

	reschan := make(chan queryResult)
	for {
		for i := 0; i < len(servers) && pending < maxConcurrentRequests; i++ {
			addr := servers[i]
			lastQuery, ok := r.lastQueries[addr.PublicKey()]
			// Do not attempt to connect query peers we recently queried
			if ok && now.Sub(lastQuery) < minTimeBetweenQueries {
				continue
			}

			pending++

			if len(r.lastQueries) >= lastQueriesCacheSize {
				expire(r.lastQueries)
			}

			r.lastQueries[addr.PublicKey()] = time.Now()

			go pingThenGetAddresses(r.disc, addr, reschan)
		}

		if pending == 0 {
			return out
		}

		select {
		case cr := <-reschan:
			pending--
			if cr.err != nil {
				//todo: consider error and maybe remove
				//todo: count failed queries and remove not functioning
				r.logger.Warning("Peer %v didn't response to protocol queries - err:%v", cr.src.String(), cr.err)
				continue
			}
			if cr.res != nil && len(cr.res) > 0 {
				for _, a := range cr.res {
					if _, ok := seen[a.PublicKey()]; ok {
						continue
					}

					if _, ok := r.lastQueries[a.PublicKey()]; ok {
						continue
					}
					out = append(out, a)
					r.book.AddAddress(a, cr.src)
					seen[a.PublicKey()] = struct{}{}
				}
			}
		case <-ctx.Done():
			return nil
		}

	}
}
