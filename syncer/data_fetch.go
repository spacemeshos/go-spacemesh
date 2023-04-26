package syncer

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/rand"
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

	logger log.Log
	msh    meshProvider
	ids    idProvider
}

// NewDataFetch creates a new DataFetch instance.
func NewDataFetch(msh meshProvider, fetch fetcher, ids idProvider, lg log.Log) *DataFetch {
	return &DataFetch{
		fetcher: fetch,
		logger:  lg,
		msh:     msh,
		ids:     ids,
	}
}

// PollMaliciousProofs polls all peers for malicious NodeIDs.
func (d *DataFetch) PollMaliciousProofs(ctx context.Context) error {
	peers := d.fetcher.GetPeers()
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
		peers = d.fetcher.GetPeers()
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

func (d *DataFetch) receiveMaliciousIDs(ctx context.Context, req *maliciousIDRequest, peer p2p.Peer, data []byte, peerErr error) {
	logger := d.logger.WithContext(ctx).WithFields(req.lid, log.Stringer("peer", peer))
	logger.Debug("received layer data from peer")
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

func (d *DataFetch) receiveData(ctx context.Context, req *dataRequest, peer p2p.Peer, data []byte, peerErr error) {
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

func fetchMalfeasanceProof(ctx context.Context, logger log.Log, ids idProvider, fetcher fetcher, req *maliciousIDRequest, data *fetch.MaliciousIDs) {
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

func fetchLayerData(ctx context.Context, logger log.Log, fetcher fetcher, req *dataRequest, data *fetch.LayerData) {
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

// PollLayerOpinions polls all peers for opinions in the specified layer.
func (d *DataFetch) PollLayerOpinions(ctx context.Context, lid types.LayerID) ([]*fetch.LayerOpinion, error) {
	peers := d.fetcher.GetPeers()
	if len(peers) == 0 {
		return nil, errNoPeers
	}

	logger := d.logger.WithContext(ctx).WithFields(lid)
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
	}
	if err := d.fetcher.GetLayerOpinions(ctx, peers, lid, okFunc, errFunc); err != nil {
		return nil, err
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
			return req.response.opinions, candidateErr
		case <-ctx.Done():
			logger.Warning("request timed out")
			return nil, errTimeout
		}
	}
}

func (d *DataFetch) receiveOpinions(ctx context.Context, req *opinionRequest, peer p2p.Peer, data []byte, peerErr error) {
	logger := d.logger.WithContext(ctx).WithFields(req.lid, log.Stringer("peer", peer))
	logger.Debug("received layer opinions from peer")

	var (
		result = peerResult[fetch.LayerOpinion]{peer: peer, err: peerErr}
		lo     fetch.LayerOpinion
	)
	if peerErr != nil {
		logger.With().Debug("received peer error for layer opinions", log.Err(peerErr))
	} else if result.err = codec.Decode(data, &lo); result.err != nil {
		logger.With().Debug("error converting bytes to LayerOpinion", log.Err(result.err))
	} else {
		lo.SetPeer(peer)
		result.data = &lo
	}
	select {
	case req.ch <- result:
	case <-ctx.Done():
		logger.Warning("request timed out")
	}
}

// GetEpochATXs fetches all ATXs in the specified epoch from a peer.
func (d *DataFetch) GetEpochATXs(ctx context.Context, epoch types.EpochID) error {
	peers := d.fetcher.GetPeers()
	if len(peers) == 0 {
		return errNoPeers
	}
	peer := peers[rand.Intn(len(peers))]
	ed, err := d.fetcher.PeerEpochInfo(ctx, peer, epoch)
	if err != nil {
		return fmt.Errorf("get epoch info (peer %v): %w", peer, err)
	}
	d.fetcher.RegisterPeerHashes(peer, types.ATXIDsToHashes(ed.AtxIDs))
	if err := d.fetcher.GetAtxs(ctx, ed.AtxIDs); err != nil {
		return fmt.Errorf("get ATXs: %w", err)
	}
	return nil
}
