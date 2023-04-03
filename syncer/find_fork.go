package syncer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

var (
	ErrPeerMeshChangedMidSession = errors.New("peer mesh changed mid session")
	ErrNodeMeshChangedMidSession = errors.New("node mesh changed mid session")
)

type layerHash struct {
	layer   types.LayerID
	hash    types.Hash32
	created time.Time
}

// boundary is used to define the to and from layers in the hash mesh requests to peers.
// the hashes of the boundary layers are known to the node and are used to double-check that
// the peer has not changed its opinions on those layers.
// if the boundary hashes change during a fork-finding session, the session is aborted.
type boundary struct {
	from, to *layerHash
}

func (b *boundary) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint32("from", b.from.layer.Uint32())
	encoder.AddString("from_hash", b.from.hash.String())
	encoder.AddUint32("to", b.to.layer.Uint32())
	encoder.AddString("to_hash", b.to.hash.String())
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
	// used to make sure we only resync based on the same layer hash once across runs.
	resynced map[types.LayerID]map[types.Hash32]time.Time
}

func NewForkFinder(lg log.Log, db sql.Executor, f fetcher, maxHashes uint32, maxStale time.Duration) *ForkFinder {
	return &ForkFinder{
		logger:           lg,
		db:               db,
		fetcher:          f,
		maxHashesInReq:   maxHashes,
		maxStaleDuration: maxStale,
		agreedPeers:      make(map[p2p.Peer]*layerHash),
		resynced:         make(map[types.LayerID]map[types.Hash32]time.Time),
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

	peers := ff.fetcher.GetPeers()
	uniquePeers := make(map[p2p.Peer]struct{})
	for _, p := range peers {
		uniquePeers[p] = struct{}{}
	}
	for p, lh := range ff.agreedPeers {
		if _, ok := uniquePeers[p]; !ok {
			if time.Since(lh.created) >= ff.maxStaleDuration {
				delete(ff.agreedPeers, p)
			}
		}
	}
	for lid, val := range ff.resynced {
		for hash, created := range val {
			if time.Since(created) >= ff.maxStaleDuration {
				delete(ff.resynced[lid], hash)
				if len(ff.resynced[lid]) == 0 {
					delete(ff.resynced, lid)
				}
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

func (ff *ForkFinder) AddResynced(lid types.LayerID, hash types.Hash32) {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	if _, ok := ff.resynced[lid]; !ok {
		ff.resynced[lid] = make(map[types.Hash32]time.Time)
	}
	ff.resynced[lid][hash] = time.Now()
}

func (ff *ForkFinder) NeedResync(lid types.LayerID, hash types.Hash32) bool {
	ff.mu.Lock()
	defer ff.mu.Unlock()
	if _, ok := ff.resynced[lid]; ok {
		_, resynced := ff.resynced[lid][hash]
		return !resynced
	}
	return true
}

// FindFork finds the point of divergence in layer opinions between the node and the specified peer
// from a given disagreed layer.
func (ff *ForkFinder) FindFork(ctx context.Context, peer p2p.Peer, diffLid types.LayerID, diffHash types.Hash32) (types.LayerID, error) {
	logger := ff.logger.WithContext(ctx).WithFields(
		log.Stringer("diff_layer", diffLid),
		log.Stringer("diff_hash", diffHash),
		log.Stringer("peer", peer))
	logger.With().Info("begin fork finding with peer")

	bnd, err := ff.setupBoundary(peer, &layerHash{layer: diffLid, hash: diffHash})
	if err != nil {
		return types.LayerID{}, err
	}

	numReqs := 0
	if bnd.from.layer.Add(1) == bnd.to.layer {
		logger.With().Info("found hash fork with peer",
			log.Stringer("fork", bnd.from.layer),
			log.Stringer("fork_hash", bnd.from.hash),
			log.Stringer("after_fork", bnd.to.layer),
			log.Stringer("after_fork_hash", bnd.to.hash),
		)
		return bnd.from.layer, nil
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
				if latestSame != nil && lid == latestSame.layer.Add(1) {
					lg.With().Info("found hash fork with peer",
						log.Int("num_reqs", numReqs),
						log.Stringer("fork", latestSame.layer),
						log.Stringer("fork_hash", latestSame.hash),
						log.Stringer("after_fork", lid),
						log.Stringer("after_fork_hash", hash))
					return latestSame.layer, nil
				}
				oldestDiff = &layerHash{layer: lid, hash: hash}
				break
			}
			latestSame = &layerHash{layer: lid, hash: hash}
			ff.updateAgreement(peer, latestSame, time.Now())
		}
		if latestSame == nil || oldestDiff == nil {
			// every layer hash is different/same from node's. this can only happen when
			// the node's local hashes change while the mesh hash request is running
			ff.Purge(true)
			return types.LayerID{}, ErrNodeMeshChangedMidSession
		}
		bnd, err = ff.setupBoundary(peer, oldestDiff)
		if err != nil {
			return types.LayerID{}, err
		}
	}
}

// UpdateAgreement updates the layer at which the peer agreed with the node.
func (ff *ForkFinder) UpdateAgreement(peer p2p.Peer, lid types.LayerID, hash types.Hash32, created time.Time) {
	ff.updateAgreement(peer, &layerHash{layer: lid, hash: hash}, created)
}

func (ff *ForkFinder) updateAgreement(peer p2p.Peer, update *layerHash, created time.Time) {
	if update == nil {
		ff.logger.With().Fatal("invalid arg", log.Stringer("peer", peer))
	}

	ff.mu.Lock()
	defer ff.mu.Unlock()
	// unconditional update instead of comparing layers because peers can change its opinions on historical layers.
	ff.agreedPeers[peer] = &layerHash{
		layer:   update.layer,
		hash:    update.hash,
		created: created,
	}
}

// setupBoundary sets up the boundary for the hash requests.
// boundary.from contains the latest layer node and peer agree on hash.
// boundary.to contains the oldest layer node and peer disagree on hash.
func (ff *ForkFinder) setupBoundary(peer p2p.Peer, oldestDiff *layerHash) (*boundary, error) {
	ff.mu.Lock()
	defer ff.mu.Unlock()

	var bnd boundary
	lastAgreed := ff.agreedPeers[peer]
	if lastAgreed != nil {
		if lastAgreed.layer.Before(oldestDiff.layer) {
			// double check if the node still has the same hash
			nodeHash, err := layers.GetAggregatedHash(ff.db, lastAgreed.layer)
			if err != nil {
				return nil, fmt.Errorf("find fork get boundary hash %v: %w", lastAgreed.layer, err)
			}
			if nodeHash == lastAgreed.hash {
				bnd.from = lastAgreed
			} else {
				delete(ff.agreedPeers, peer)
			}
		} else {
			delete(ff.agreedPeers, peer)
		}
	}
	if bnd.from == nil {
		glid := types.GetEffectiveGenesis()
		ghash, err := layers.GetAggregatedHash(ff.db, glid)
		if err != nil {
			return nil, fmt.Errorf("find fork get genesis hash: %w", err)
		}
		bnd.from = &layerHash{layer: glid, hash: ghash}
	}
	bnd.to = oldestDiff
	return &bnd, nil
}

// get layer hashes from peers in the range defined by the boundary.
// if the number of hashes is less than maxHashesInReq, then request every hash.
// otherwise, set appropriate params such that the number of hashes requested is maxHashesInReq
// while ensuring hashes for the boundary layers are requested.
func (ff *ForkFinder) sendRequest(ctx context.Context, logger log.Log, peer p2p.Peer, bnd *boundary) (*fetch.MeshHashes, error) {
	if bnd == nil {
		logger.Fatal("invalid args")
	} else if bnd.from == nil || bnd.to == nil || !bnd.to.layer.After(bnd.from.layer) {
		logger.With().Fatal("invalid args", log.Object("boundary", bnd))
	}

	var delta, steps uint32
	dist := bnd.to.layer.Difference(bnd.from.layer)
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
		From:  bnd.from.layer,
		To:    bnd.to.layer,
		Delta: delta,
		Steps: steps,
	}
	logger.With().Debug("sending request", log.Uint32("delta", delta), log.Uint32("steps", steps))
	mh, err := ff.fetcher.PeerMeshHashes(ctx, peer, req)
	if err != nil {
		return nil, fmt.Errorf("find fork hash req: %w", err)
	}
	logger.With().Debug("received response",
		log.Int("num_layers", len(mh.Layers)),
		log.Int("num_hashes", len(mh.Hashes)))
	if len(mh.Layers) != len(mh.Hashes) || uint32(len(mh.Layers)) != steps+1 {
		return nil, errors.New("inconsistent layers for mesh hashes")
	}
	if mh.Layers[0] != bnd.from.layer || mh.Layers[steps] != bnd.to.layer {
		logger.With().Warning("invalid boundary in response",
			log.Stringer("rsp_from", mh.Layers[0]),
			log.Stringer("rsp_to", mh.Layers[steps]))
		return nil, errors.New("invalid boundary in response")
	}
	if mh.Hashes[0] != bnd.from.hash || mh.Hashes[steps] != bnd.to.hash {
		logger.With().Warning("peer boundary hashes have changed",
			log.Stringer("hash_from", bnd.from.hash),
			log.Stringer("hash_to", bnd.to.hash),
			log.Stringer("peer_hash_from", mh.Hashes[0]),
			log.Stringer("peer_hash_to", mh.Hashes[steps]))
		ff.Purge(false, peer)
		return nil, ErrPeerMeshChangedMidSession
	}
	return mh, nil
}
