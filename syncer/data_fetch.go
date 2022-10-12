package syncer

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
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
	blocks  map[types.BlockID]struct{}
	err     error
}

type opinionResponse struct {
	opinions []*fetch.LayerOpinion
	err      error
}

type (
	dataRequest    request[fetch.LayerData, dataResponse]
	opinionRequest request[fetch.LayerOpinion, opinionResponse]
)

// DataFetch contains the logic of fetching mesh data.
type DataFetch struct {
	logger  log.Log
	cdb     *datastore.CachedDB
	msh     meshProvider
	fetcher fetcher
}

// NewDataFetch creates a new DataFetch instance.
func NewDataFetch(cdb *datastore.CachedDB, msh meshProvider, fetch fetcher, lg log.Log) *DataFetch {
	return &DataFetch{
		logger:  lg,
		cdb:     cdb,
		msh:     msh,
		fetcher: fetch,
	}
}

// PollLayerData polls all peers for data in the specified layer.
func (d *DataFetch) PollLayerData(ctx context.Context, lid types.LayerID) error {
	peers := d.fetcher.GetPeers()
	if len(peers) == 0 {
		return errNoPeers
	}

	req := &dataRequest{
		lid:   lid,
		peers: peers,
		response: dataResponse{
			ballots: map[types.BallotID]struct{}{},
			blocks:  map[types.BlockID]struct{}{},
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
	logger := d.logger.WithContext(ctx).WithFields(lid)
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
				fetchLayerData(ctx, logger, d.fetcher, req, res.data)
			} else if candidateErr == nil {
				candidateErr = res.err
			}
			if len(req.peerResults) < len(req.peers) {
				break
			}
			// all peer responded
			if !success {
				req.response.err = candidateErr
			}
			if req.response.err == nil && len(req.response.blocks) == 0 {
				d.msh.SetZeroBlockLayer(ctx, req.lid)
			}
			return req.response.err
		case <-ctx.Done():
			return errTimeout
		}
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
	for _, blkID := range data.Blocks {
		layerHashes = append(layerHashes, blkID.AsHash32())
	}
	if len(layerHashes) == 0 {
		return
	}
	fetcher.RegisterPeerHashes(peer, layerHashes)
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
	var blocksToFetch []types.BlockID
	for _, blkID := range data.Blocks {
		if _, ok := req.response.blocks[blkID]; !ok {
			// not yet fetched
			req.response.blocks[blkID] = struct{}{}
			blocksToFetch = append(blocksToFetch, blkID)
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

	if len(blocksToFetch) > 0 {
		logger.With().Debug("fetching new blocks", log.Int("to_fetch", len(blocksToFetch)))
		if err := fetcher.GetBlocks(ctx, blocksToFetch); err != nil {
			logger.With().Warning("failed fetching new blocks",
				log.Array("block_ids", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
					for _, bid := range blocksToFetch {
						encoder.AppendString(bid.String())
					}
					return nil
				})),
				log.Err(err))
			// syntactically invalid blocks are expected from malicious peers
		}
	}
}

// PollLayerOpinions polls all peers for opinions in the specified layer.
func (d *DataFetch) PollLayerOpinions(ctx context.Context, lid types.LayerID) ([]*fetch.LayerOpinion, error) {
	peers := d.fetcher.GetPeers()
	if len(peers) == 0 {
		return nil, errNoPeers
	}
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
			if !success {
				req.response.err = candidateErr
			}
			return req.response.opinions, req.response.err
		case <-ctx.Done():
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
	}
}

type epochAtxRes struct {
	err    error
	atxIDs []types.ATXID
}

func randomPeer(peers []p2p.Peer) p2p.Peer {
	idx := rand.Intn(len(peers))
	return peers[idx]
}

// GetEpochATXs fetches all ATXs in the specified epoch from a peer.
func (d *DataFetch) GetEpochATXs(ctx context.Context, epoch types.EpochID) error {
	resCh := make(chan epochAtxRes, 1)
	peers := d.fetcher.GetPeers()
	if len(peers) == 0 {
		return errNoPeers
	}
	peer := randomPeer(peers)
	okFunc := func(data []byte) {
		atxIDs, err := codec.DecodeSlice[types.ATXID](data)
		resCh <- epochAtxRes{
			err:    err,
			atxIDs: atxIDs,
		}
	}
	errFunc := func(err error) {
		resCh <- epochAtxRes{
			err: err,
		}
	}
	if err := d.fetcher.GetEpochATXIDs(ctx, peer, epoch, okFunc, errFunc); err != nil {
		return fmt.Errorf("get ATXIDs (peer %v): %w", peer, err)
	}
	var res epochAtxRes
	select {
	case res = <-resCh:
		break
	case <-ctx.Done():
		return errTimeout
	}
	if res.err != nil {
		return res.err
	}

	d.fetcher.RegisterPeerHashes(peer, types.ATXIDsToHashes(res.atxIDs))
	if err := d.fetcher.GetAtxs(ctx, res.atxIDs); err != nil {
		return fmt.Errorf("get ATXs: %w", err)
	}
	return nil
}

// GetBlocks fetches all blocks specified in the list of BlockID.
func (d *DataFetch) GetBlocks(ctx context.Context, bids []types.BlockID) error {
	return d.fetcher.GetBlocks(ctx, bids)
}
