package syncer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

var ErrPeerMeshChangedMidSession = errors.New("peer mesh changed mid session")

type layerHash struct {
	lid     types.LayerID
	hash    types.Hash32
	created time.Time
}

func newLayerHash(lid types.LayerID, hash types.Hash32) *layerHash {
	return &layerHash{
		lid:  lid,
		hash: hash,
	}
}

// boundary is used to define the to and from layers in the hash mesh requests to peers.
// the hashes of the boundary layers are known to the node and are used to double-check that
// the peer has not changed its opinions on those layers.
// if the boundary hashes change during the fork-finding session, the session is aborted.
type boundary [2]*layerHash

func newBoundary() *boundary {
	return &boundary{
		newLayerHash(types.LayerID{}, types.Hash32{}),
		newLayerHash(types.LayerID{}, types.Hash32{}),
	}
}

func (b *boundary) setFrom(lh *layerHash) {
	b[0] = lh
}

func (b *boundary) setTo(lh *layerHash) {
	b[1] = lh
}

func (b *boundary) fromLayer() types.LayerID {
	return b[0].lid
}

func (b *boundary) fomHash() types.Hash32 {
	return b[0].hash
}

func (b *boundary) toLayer() types.LayerID {
	return b[1].lid
}

func (b *boundary) toHash() types.Hash32 {
	return b[1].hash
}

func (b *boundary) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint32("from", b.fromLayer().Uint32())
	encoder.AddString("from_hash", b.fomHash().ShortString())
	encoder.AddUint32("to", b.toLayer().Uint32())
	encoder.AddString("to_hash", b.toHash().ShortString())
	return nil
}

type ForkFinder struct {
	logger           log.Log
	db               sql.Executor
	fetcher          fetcher
	maxHashesInReq   uint32
	maxStaleDuration time.Duration

	mu          sync.Mutex
	agreedPeers map[p2p.Peer]*layerHash
}

func NewForkFinder(lg log.Log, db sql.Executor, f fetcher, maxHashes uint32, maxStale time.Duration) *ForkFinder {
	return &ForkFinder{
		logger:           lg,
		db:               db,
		fetcher:          f,
		maxHashesInReq:   maxHashes,
		maxStaleDuration: maxStale,
		agreedPeers:      make(map[p2p.Peer]*layerHash),
	}
}

// Purge cached agreements with peers.
func (ff *ForkFinder) Purge(all bool, toPurge ...p2p.Peer) {
	ff.mu.Lock()
	defer ff.mu.Unlock()

	if all {
		ff.agreedPeers = make(map[p2p.Peer]*layerHash)
		return
	}

	if len(toPurge) > 0 {
		for _, p := range toPurge {
			delete(ff.agreedPeers, p)
		}
		return
	}

	uniquePeers := util.SliceToSetStringer(ff.fetcher.GetPeers())
	for p, lh := range ff.agreedPeers {
		if _, ok := uniquePeers[p.String()]; !ok {
			if time.Since(lh.created) >= ff.maxStaleDuration {
				delete(ff.agreedPeers, p)
			}
		}
	}
}

// NumPeersCached returns the number of peer agreement cached.
func (ff *ForkFinder) NumPeersCached() int {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	return len(ff.agreedPeers)
}

// FindFork finds the point of divergence in layer opinions between the node and the specified peer
// from a given disagreed layer.
func (ff *ForkFinder) FindFork(ctx context.Context, peer p2p.Peer, diffLid types.LayerID, diffHash types.Hash32) (types.LayerID, error) {
	logger := ff.logger.WithContext(ctx).WithFields(
		log.Stringer("diff_layer", diffLid),
		log.Stringer("diff_hash", diffHash),
		log.Stringer("peer", peer))
	logger.With().Info("finding hash fork with peer")

	bnd, err := ff.setupBoundary(peer, newLayerHash(diffLid, diffHash))
	if err != nil {
		return types.LayerID{}, err
	}

	numReqs := 0
	if bnd.fromLayer().Add(1) == bnd.toLayer() {
		logger.With().Info("found hash fork with peer",
			log.Stringer("before_fork", bnd.fromLayer()),
			log.Stringer("before_fork_hash", bnd.fomHash()),
			log.Stringer("forked", bnd.toLayer()),
			log.Stringer("forked_hash", bnd.toHash()),
		)
		return bnd.fromLayer(), nil
	}

	for {
		lg := logger.WithFields(log.Object("boundary", bnd))
		mh, err := ff.sendRequest(ctx, lg, peer, bnd)
		numReqs++
		if err != nil {
			lg.With().Error("failed hash request", log.Err(err))
			return types.LayerID{}, err
		}

		ownHashes, err := layers.GetAggHashes(ff.db, mh.Layers)
		if err != nil {
			lg.With().Error("failed own hashes lookup", log.Err(err))
			return types.LayerID{}, err
		}

		var latestSame, oldestDiff *layerHash
		for i, hash := range mh.Hashes {
			lid := mh.Layers[i]
			ownHash := ownHashes[i]
			if ownHash != hash {
				if lid == latestSame.lid.Add(1) {
					lg.With().Info("found hash fork with peer",
						log.Int("num_reqs", numReqs),
						log.Stringer("before_fork", latestSame.lid),
						log.Stringer("before_fork_hash", latestSame.hash),
						log.Stringer("forked", lid),
						log.Stringer("forked_hash", hash))
					return latestSame.lid, nil
				}
				oldestDiff = newLayerHash(lid, hash)
				break
			}
			latestSame = newLayerHash(lid, hash)
			ff.updateAgreement(peer, latestSame, time.Now())
		}
		if latestSame == nil || oldestDiff == nil {
			// every layer hash is different/same from node's. this can only happen when
			// the node's local hashes change while the mesh hash request is running
			ff.Purge(true)
			return types.LayerID{}, errors.New("node or peer hashes changed during request")
		}
		bnd, err = ff.setupBoundary(peer, oldestDiff)
		if err != nil {
			return types.LayerID{}, err
		}
	}
}

// UpdateAgreement updates the layer at which the peer agreed with the node.
func (ff *ForkFinder) UpdateAgreement(peer p2p.Peer, lid types.LayerID, hash types.Hash32, created time.Time) {
	ff.updateAgreement(peer, newLayerHash(lid, hash), created)
}

func (ff *ForkFinder) updateAgreement(peer p2p.Peer, update *layerHash, created time.Time) {
	if update == nil {
		ff.logger.With().Fatal("invalid arg", log.Stringer("peer", peer))
	}

	ff.mu.Lock()
	defer ff.mu.Unlock()
	// unconditional update instead of comparing layers because peers can change its opinions on historical layers.
	ff.agreedPeers[peer] = &layerHash{
		lid:     update.lid,
		hash:    update.hash,
		created: created,
	}
}

// setupBoundary sets up the boundary for the hash requests.
// boundary[0] contains the latest layer node and peer agree on hash.
// boundary[1] contains the oldest layer node and peer disagree on hash.
func (ff *ForkFinder) setupBoundary(peer p2p.Peer, oldestDiff *layerHash) (*boundary, error) {
	ff.mu.Lock()
	defer ff.mu.Unlock()

	bnd := newBoundary()
	lastAgreed := ff.agreedPeers[peer]
	if lastAgreed != nil {
		// double check if the node still has the same hash
		nodeHash, err := layers.GetAggregatedHash(ff.db, lastAgreed.lid)
		if err != nil {
			return bnd, fmt.Errorf("find fork get boundary hash %v: %w", lastAgreed.lid, err)
		}
		if nodeHash == lastAgreed.hash {
			bnd.setFrom(lastAgreed)
		} else {
			delete(ff.agreedPeers, peer)
		}
	}
	if bnd.fromLayer() == (types.LayerID{}) {
		glid := types.GetEffectiveGenesis()
		ghash, err := layers.GetAggregatedHash(ff.db, glid)
		if err != nil {
			return bnd, fmt.Errorf("find fork get genesis hash: %w", err)
		}
		bnd.setFrom(newLayerHash(glid, ghash))
	}
	bnd.setTo(oldestDiff)
	return bnd, nil
}

// get layer hashes from peers in the range defined by the boundary.
// if the number of hashes is less than maxHashesInReq, then request every hash.
// otherwise, set appropriate params such that the number of hashes requested is maxHashesInReq
// while ensuring hashes for the boundary layers are requested.
func (ff *ForkFinder) sendRequest(ctx context.Context, logger log.Log, peer p2p.Peer, bnd *boundary) (*fetch.MeshHashes, error) {
	from := bnd[0]
	to := bnd[1]
	if !to.lid.After(from.lid) {
		logger.Fatal("invalid args")
	}
	var delta, steps uint32
	dist := to.lid.Difference(from.lid)
	if dist+1 <= ff.maxHashesInReq {
		steps = dist
		delta = 1
	} else {
		steps = uint32(ff.maxHashesInReq) - 1
		delta = dist / steps
		if dist%steps > 0 {
			delta++
		}
		// adjustment to delta may reduce the number of steps required
		for ; delta*(steps-1) >= dist; steps-- {
		}
	}
	req := &fetch.MeshHashRequest{
		From:  from.lid,
		To:    to.lid,
		Delta: delta,
		Steps: steps,
	}
	logger.With().Debug("sending request", log.Uint32("delta", delta), log.Uint32("steps", steps))
	mh, err := ff.fetcher.PeerMeshHashes(ctx, peer, req)
	if err != nil {
		return nil, fmt.Errorf("find fork hash req: %w", err)
	}
	if len(mh.Layers) != len(mh.Hashes) || uint32(len(mh.Layers)) != steps+1 {
		return nil, errors.New("inconsistent layers for mesh hashes")
	}

	logger.With().Debug("received response", log.Int("num_hashes", len(mh.Hashes)))
	if mh.Layers[0] != from.lid || mh.Layers[steps] != to.lid {
		logger.With().Warning("invalid boundary in response",
			log.Stringer("rsp_from", mh.Layers[0]),
			log.Stringer("rsp_to", mh.Layers[steps]))
		return nil, errors.New("invalid boundary in response")
	}
	if mh.Hashes[0] != from.hash || mh.Hashes[steps] != to.hash {
		logger.With().Warning("peer boundary hashes have changed",
			log.Stringer("hash_from", from.hash),
			log.Stringer("hash_to", to.hash),
			log.Stringer("peer_hash_from", mh.Hashes[0]),
			log.Stringer("peer_hash_to", mh.Hashes[steps]))
		ff.Purge(false, peer)
		return nil, ErrPeerMeshChangedMidSession
	}
	return mh, nil
}
