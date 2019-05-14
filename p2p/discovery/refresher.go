package discovery

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
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
	logger log.Log
	config config.SwarmConfig

	bootNodes []NodeInfo

	book *addrBook

	lastQueries map[p2pcrypto.PublicKey]time.Time

	disc Protocol

	quit chan struct{}
}

func newRefresher(book *addrBook, disc Protocol, config config.SwarmConfig, logger log.Log) *refresher {
	bn := make([]NodeInfo, 0, len(config.BootstrapNodes))
	for _, n := range config.BootstrapNodes {
		nd, err := node.NewNodeFromString(n)
		if err != nil {
			// TODO : handle errors
			continue
		}
		bn = append(bn, NodeInfoFromNode(nd, nd.Address()))
	}

	//todo: trigger requestAddresses every X with random nodes

	return &refresher{
		logger:      logger,
		book:        book,
		disc:        disc,
		bootNodes:   bn,
		lastQueries: make(map[p2pcrypto.PublicKey]time.Time),
		quit:        make(chan struct{}), // todo: context ?
	}
}

func (r *refresher) Bootstrap(ctx context.Context, minPeers int) error {
	var err error
	tries := 0
	servers := make([]NodeInfo, 0, len(r.bootNodes))

	// The following loop will add the pre-set bootstrap nodes and query them for results
	// if there were any results but we didn't reach minPeers it will try the same procedure
	// using the results as servers to query. every failed loop will wait a backoff
	// to let other nodes populate before flooding with queries.

loop:
	for {
		size := r.book.NumAddresses()
		if size == 0 {
			r.book.AddAddresses(r.bootNodes, r.book.localAddress)
			servers = r.bootNodes
		}

		if size < minPeers+len(r.bootNodes) {
			r.logger.Info("Bootstrap: starting with %v sized table", size)
			res := r.requestAddresses(servers)
			if len(res) > 0 {
				servers = res
			}
			tries++
			r.logger.Info("Bootstrap : %d try gave %v results", tries, len(res))
		}

		newsize := r.book.NumAddresses()
		if newsize >= minPeers+len(r.bootNodes) {
			r.logger.Info("Stopping bootstrap, achieved %v need %v", newsize-len(r.bootNodes), minPeers)
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

	// currently we only have  the discovery address of bootnodes in the configuration so let them pick their own neighbors.
	for _, b := range r.bootNodes {
		r.book.RemoveAddress(b.PublicKey())
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
	src NodeInfo
	res []NodeInfo
	err error
}

// pingThenGetAddresses is sending a ping, then find node, then return results on given chan.
func pingThenGetAddresses(p Protocol, addr NodeInfo, qr chan queryResult) {
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
func (r *refresher) requestAddresses(servers []NodeInfo) []NodeInfo {

	// todo: here we stop only after we've tried querying or queried all addrs
	// 	maybe we should stop after we've reached a certain amount ? (needMoreAddresses..)
	var out []NodeInfo

	seen := make(map[p2pcrypto.PublicKey]struct{})
	seen[r.book.localAddress.PublicKey()] = struct{}{}

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
				r.logger.Warning("Peer %v didn't response to protocol queries - err:%v", cr.src.Pretty(), cr.err)
				continue
			}
			if cr.res != nil && len(cr.res) > 0 {
				for _, a := range cr.res {
					if _, ok := seen[a.PublicKey()]; !ok {
						out = append(out, a)
						r.book.AddAddress(a, cr.src)
						seen[a.PublicKey()] = struct{}{}
					}
				}
			}
		case <-r.quit:
			return nil
		}

	}
}
