package syncer

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

var (
	errNoPeers = errors.New("no peers")
	errTimeout = errors.New("request timeout")
)

type peerResult[T any] struct {
	peer p2p.Peer
	data *T
	err  error
}

type request[T any, R any] struct {
	lid         types.LayerID
	peers       []p2p.Peer
	response    R
	ch          chan peerResult[T]
	peerResults map[p2p.Peer]peerResult[T]
}

type dataResponse struct {
	ballots map[types.BallotID]struct{}
}

type opinionResponse struct {
	opinions []*fetch.LayerOpinion
}

type maliciousIDResponse struct {
	ids map[types.NodeID]struct{}
}

type (
	dataRequest        request[fetch.LayerData, dataResponse]
	opinionRequest     request[fetch.LayerOpinion, opinionResponse]
	maliciousIDRequest request[fetch.MaliciousIDs, maliciousIDResponse]
)

// DataFetch contains the logic of fetching mesh data.
type DataFetch struct {
	fetcher

	logger  log.Log
	msh     meshProvider
	ids     idProvider
	asCache activeSetCache

	mu        sync.Mutex
	atxSynced map[types.EpochID]map[p2p.Peer]struct{}
}

// NewDataFetch creates a new DataFetch instance.
func NewDataFetch(
	msh meshProvider,
	fetch fetcher,
	ids idProvider,
	cache activeSetCache,
	lg log.Log,
) *DataFetch {
	return &DataFetch{
		fetcher:   fetch,
		logger:    lg,
		msh:       msh,
		ids:       ids,
		asCache:   cache,
		atxSynced: map[types.EpochID]map[p2p.Peer]struct{}{},
	}
}

// PollMaliciousProofs polls all peers for malicious NodeIDs.
func (d *DataFetch) PollMaliciousProofs(ctx context.Context) error {
	peers := d.fetcher.SelectBestShuffled(fetch.RedundantPeers)
	logger := d.logger.WithContext(ctx)
	req := &maliciousIDRequest{
		peers: peers,
		response: maliciousIDResponse{
			ids: map[types.NodeID]struct{}{},
		},
		ch: make(chan peerResult[fetch.MaliciousIDs], len(peers)),
	}
	okFunc := func(data []byte, peer p2p.Peer) {
		d.receiveMaliciousIDs(ctx, req, peer, data, nil)
	}
	errFunc := func(err error, peer p2p.Peer) {
		d.receiveMaliciousIDs(ctx, req, peer, nil, err)
		malPeerError.Inc()
	}
	if err := d.fetcher.GetMaliciousIDs(ctx, peers, okFunc, errFunc); err != nil {
		return err
	}

	req.peerResults = map[p2p.Peer]peerResult[fetch.MaliciousIDs]{}
	var (
		success      bool
		candidateErr error
	)
	for {
		select {
		case res := <-req.ch:
			logger.Debug("received malicious IDs")
			req.peerResults[res.peer] = res
			if res.err == nil {
				success = true
				fetchMalfeasanceProof(ctx, logger, d.ids, d.fetcher, req, res.data)
			} else if candidateErr == nil {
				candidateErr = res.err
			}
			if len(req.peerResults) < len(req.peers) {
				break
			}
			// all peer responded
			if success {
				candidateErr = nil
			}
			return candidateErr
		case <-ctx.Done():
			logger.Warning("request timed out")
			return errTimeout
		}
	}
}

// PollLayerData polls all peers for data in the specified layer.
func (d *DataFetch) PollLayerData(ctx context.Context, lid types.LayerID, peers ...p2p.Peer) error {
	if len(peers) == 0 {
		peers = d.fetcher.SelectBestShuffled(fetch.RedundantPeers)
	}
	if len(peers) == 0 {
		return errNoPeers
	}

	logger := d.logger.WithContext(ctx).WithFields(lid)
	req := &dataRequest{
		lid:   lid,
		peers: peers,
		response: dataResponse{
			ballots: map[types.BallotID]struct{}{},
		},
		ch: make(chan peerResult[fetch.LayerData], len(peers)),
	}
	okFunc := func(data []byte, peer p2p.Peer) {
		d.receiveData(ctx, req, peer, data, nil)
	}
	errFunc := func(err error, peer p2p.Peer) {
		d.receiveData(ctx, req, peer, nil, err)
		layerPeerError.Inc()
	}
	if err := d.fetcher.GetLayerData(ctx, peers, lid, okFunc, errFunc); err != nil {
		return err
	}

	req.peerResults = map[p2p.Peer]peerResult[fetch.LayerData]{}
	var (
		success      bool
		candidateErr error
	)
	for {
		select {
		case res := <-req.ch:
			logger.Debug("received layer data")
			req.peerResults[res.peer] = res
			if res.err == nil {
				success = true
				logger.Debug("fetching layer data")
				fetchLayerData(ctx, logger, d.fetcher, req, res.data)
				logger.Debug("fetched layer data")
			} else if candidateErr == nil {
				candidateErr = res.err
			}
			if len(req.peerResults) < len(req.peers) {
				break
			}
			// all peer responded
			if success {
				candidateErr = nil
			}
			return candidateErr
		case <-ctx.Done():
			logger.Warning("request timed out")
			return errTimeout
		}
	}
}

func (d *DataFetch) receiveMaliciousIDs(
	ctx context.Context,
	req *maliciousIDRequest,
	peer p2p.Peer,
	data []byte,
	peerErr error,
) {
	logger := d.logger.WithContext(ctx).WithFields(log.Stringer("peer", peer))
	logger.Debug("received malicious id from peer")
	var (
		result = peerResult[fetch.MaliciousIDs]{peer: peer, err: peerErr}
		malIDs fetch.MaliciousIDs
	)
	if peerErr != nil {
		logger.With().Debug("received peer error for layer data", req.lid, log.Err(peerErr))
	} else if result.err = codec.Decode(data, &malIDs); result.err != nil {
		logger.With().Debug("error converting bytes to LayerData", log.Err(result.err))
	} else {
		result.data = &malIDs
	}
	select {
	case req.ch <- result:
	case <-ctx.Done():
		logger.Warning("request timed out")
	}
}

func (d *DataFetch) receiveData(
	ctx context.Context,
	req *dataRequest,
	peer p2p.Peer,
	data []byte,
	peerErr error,
) {
	logger := d.logger.WithContext(ctx).WithFields(req.lid, log.Stringer("peer", peer))
	logger.Debug("received layer data from peer")
	var (
		result = peerResult[fetch.LayerData]{peer: peer, err: peerErr}
		ld     fetch.LayerData
	)
	if peerErr != nil {
		logger.With().Debug("received peer error for layer data", req.lid, log.Err(peerErr))
	} else if result.err = codec.Decode(data, &ld); result.err != nil {
		logger.With().Debug("error converting bytes to LayerData", log.Err(result.err))
	} else {
		result.data = &ld
		registerLayerHashes(d.fetcher, peer, result.data)
	}
	select {
	case req.ch <- result:
	case <-ctx.Done():
		logger.Warning("request timed out")
	}
}

// registerLayerHashes registers hashes with the peer that provides these hashes.
func registerLayerHashes(fetcher fetcher, peer p2p.Peer, data *fetch.LayerData) {
	if data == nil {
		return
	}
	var layerHashes []types.Hash32
	for _, ballotID := range data.Ballots {
		layerHashes = append(layerHashes, ballotID.AsHash32())
	}
	if len(layerHashes) == 0 {
		return
	}
	fetcher.RegisterPeerHashes(peer, layerHashes)
}

func fetchMalfeasanceProof(
	ctx context.Context,
	logger log.Log,
	ids idProvider,
	fetcher fetcher,
	req *maliciousIDRequest,
	data *fetch.MaliciousIDs,
) {
	var idsToFetch []types.NodeID
	for _, nodeID := range data.NodeIDs {
		if _, ok := req.response.ids[nodeID]; !ok {
			// check if the NodeID exists
			if exists, err := ids.IdentityExists(nodeID); err != nil {
				logger.With().Error("failed to check identity", log.Err(err))
				continue
			} else if !exists {
				logger.With().Warning("malicious identity does not exist",
					log.String("identity", nodeID.String()))
				continue
			}
			// not yet fetched
			req.response.ids[nodeID] = struct{}{}
			idsToFetch = append(idsToFetch, nodeID)
		}
	}
	if len(idsToFetch) > 0 {
		logger.With().Info("fetching malfeasance proofs", log.Int("to_fetch", len(idsToFetch)))
		if err := fetcher.GetMalfeasanceProofs(ctx, idsToFetch); err != nil {
			logger.With().Warning("failed fetching malfeasance proofs",
				log.Array("malicious_ids", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
					for _, nodeID := range idsToFetch {
						encoder.AppendString(nodeID.String())
					}
					return nil
				})),
				log.Err(err))
		}
	}
}

func fetchLayerData(
	ctx context.Context,
	logger log.Log,
	fetcher fetcher,
	req *dataRequest,
	data *fetch.LayerData,
) {
	var ballotsToFetch []types.BallotID
	for _, ballotID := range data.Ballots {
		if _, ok := req.response.ballots[ballotID]; !ok {
			// not yet fetched
			req.response.ballots[ballotID] = struct{}{}
			ballotsToFetch = append(ballotsToFetch, ballotID)
		}
	}

	if len(ballotsToFetch) > 0 {
		logger.With().Debug("fetching new ballots", log.Int("to_fetch", len(ballotsToFetch)))
		if err := fetcher.GetBallots(ctx, ballotsToFetch); err != nil {
			logger.With().Warning("failed fetching new ballots",
				log.Array("ballot_ids", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
					for _, bid := range ballotsToFetch {
						encoder.AppendString(bid.String())
					}
					return nil
				})),
				log.Err(err))

			// syntactically invalid ballots are expected from malicious peers
		}
	}
}

func (d *DataFetch) PollLayerOpinions(
	ctx context.Context,
	lid types.LayerID,
	needCert bool,
	peers []p2p.Peer,
) ([]*fetch.LayerOpinion, []*types.Certificate, error) {
	req := &opinionRequest{
		lid:   lid,
		peers: peers,
		ch:    make(chan peerResult[fetch.LayerOpinion], len(peers)),
	}
	okFunc := func(data []byte, peer p2p.Peer) {
		d.receiveOpinions(ctx, req, peer, data, nil)
	}
	errFunc := func(err error, peer p2p.Peer) {
		d.receiveOpinions(ctx, req, peer, nil, err)
		opnsPeerError.Inc()
	}
	if err := d.fetcher.GetLayerOpinions(ctx, peers, lid, okFunc, errFunc); err != nil {
		return nil, nil, err
	}
	req.peerResults = map[p2p.Peer]peerResult[fetch.LayerOpinion]{}
	var (
		success      bool
		candidateErr error
	)
	for {
		select {
		case res := <-req.ch:
			req.peerResults[res.peer] = res
			if res.err == nil {
				success = true
				req.response.opinions = append(req.response.opinions, res.data)
			} else if candidateErr == nil {
				candidateErr = res.err
			}
			if len(req.peerResults) < len(req.peers) {
				break
			}
			// all peer responded
			if success {
				candidateErr = nil
			}
			certs := make([]*types.Certificate, 0, len(req.response.opinions))
			if needCert {
				peerCerts := map[types.BlockID][]p2p.Peer{}
				for _, opns := range req.response.opinions {
					if opns.Certified == nil {
						continue
					}
					if _, ok := peerCerts[*opns.Certified]; !ok {
						peerCerts[*opns.Certified] = []p2p.Peer{}
					}
					peerCerts[*opns.Certified] = append(peerCerts[*opns.Certified], opns.Peer())
					// note that we want to fetch block certificate for types.EmptyBlockID as well
					// but we don't need to register hash for the actual block fetching
					if *opns.Certified != types.EmptyBlockID {
						d.fetcher.RegisterPeerHashes(
							opns.Peer(),
							[]types.Hash32{opns.Certified.AsHash32()},
						)
					}
				}
				for bid, bidPeers := range peerCerts {
					cert, err := d.fetcher.GetCert(ctx, lid, bid, bidPeers)
					if err != nil {
						certPeerError.Inc()
						continue
					}
					certs = append(certs, cert)
				}
			}
			return req.response.opinions, certs, candidateErr
		case <-ctx.Done():
			d.logger.WithContext(ctx).Debug("request timed out", lid)
			return nil, nil, errTimeout
		}
	}
}

func (d *DataFetch) receiveOpinions(
	ctx context.Context,
	req *opinionRequest,
	peer p2p.Peer,
	data []byte,
	peerErr error,
) {
	logger := d.logger.WithContext(ctx).WithFields(req.lid, log.Stringer("peer", peer))
	logger.Debug("received layer opinions from peer")

	var (
		result = peerResult[fetch.LayerOpinion]{peer: peer, err: peerErr}
		lo     fetch.LayerOpinion
	)
	if peerErr != nil {
		logger.With().Debug("received peer error for layer opinions", log.Err(peerErr))
	} else if result.err = codec.Decode(data, &lo); result.err != nil {
		logger.With().Debug("error decoding LayerOpinion", log.Err(result.err))
	} else {
		lo.SetPeer(peer)
		result.data = &lo
	}
	select {
	case req.ch <- result:
	case <-ctx.Done():
		logger.Debug("request timed out")
	}
}

func (d *DataFetch) pickAtxPeer(epoch types.EpochID, peers []p2p.Peer) p2p.Peer {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.atxSynced[epoch]; !ok {
		d.atxSynced[epoch] = map[p2p.Peer]struct{}{}
		delete(d.atxSynced, epoch-1)
	}
	for _, p := range peers {
		if _, ok := d.atxSynced[epoch][p]; !ok {
			return p
		}
	}
	return p2p.NoPeer
}

func (d *DataFetch) updateAtxPeer(epoch types.EpochID, peer p2p.Peer) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.atxSynced[epoch][peer] = struct{}{}
}

// GetEpochATXs fetches all ATXs published in the specified epoch from a peer.
func (d *DataFetch) GetEpochATXs(ctx context.Context, epoch types.EpochID) error {
	peers := d.fetcher.SelectBestShuffled(fetch.RedundantPeers)
	if len(peers) == 0 {
		return errNoPeers
	}
	peer := d.pickAtxPeer(epoch, peers)
	if peer == p2p.NoPeer {
		d.logger.WithContext(ctx).With().Debug("synced atxs from all peers",
			epoch,
			log.Int("peers", len(peers)),
		)
		return nil
	}

	ed, err := d.fetcher.PeerEpochInfo(ctx, peer, epoch)
	if err != nil {
		atxPeerError.Inc()
		return fmt.Errorf("get epoch info (peer %v): %w", peer, err)
	}
	if len(ed.AtxIDs) == 0 {
		d.logger.WithContext(ctx).With().Debug("peer have zero atx",
			epoch,
			log.Stringer("peer", peer),
		)
		return nil
	}
	d.updateAtxPeer(epoch, peer)
	d.fetcher.RegisterPeerHashes(peer, types.ATXIDsToHashes(ed.AtxIDs))
	missing := d.asCache.GetMissingActiveSet(epoch+1, ed.AtxIDs)
	d.logger.WithContext(ctx).With().Debug("fetching atxs",
		epoch,
		log.Stringer("peer", peer),
		log.Int("total", len(ed.AtxIDs)),
		log.Int("missing", len(missing)),
	)
	if len(missing) > 0 {
		if err := d.fetcher.GetAtxs(ctx, missing); err != nil {
			return fmt.Errorf("get ATXs: %w", err)
		}
	}
	return nil
}
