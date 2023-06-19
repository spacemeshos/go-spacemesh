package fetch

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/go-multierror"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

var errBadRequest = errors.New("invalid request")

// GetAtxs gets the data for given atx IDs and validates them. returns an error if at least one ATX cannot be fetched.
func (f *Fetch) GetAtxs(ctx context.Context, ids []types.ATXID) error {
	if len(ids) == 0 {
		return nil
	}
	f.logger.WithContext(ctx).With().Debug("requesting atxs from peer", log.Int("num_atxs", len(ids)))
	hashes := types.ATXIDsToHashes(ids)
	return f.getHashes(ctx, hashes, datastore.ATXDB, f.validators.atx.HandleMessage)
}

type dataReceiver func(context.Context, p2p.Peer, []byte) error

func (f *Fetch) getHashes(ctx context.Context, hashes []types.Hash32, hint datastore.Hint, receiver dataReceiver) error {
	var eg multierror.Group
	for _, hash := range hashes {
		p, err := f.getHash(ctx, hash, hint, receiver)
		if err != nil {
			return err
		}
		if p == nil {
			// data is available locally
			continue
		}

		h := hash
		eg.Go(func() error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-p.completed:
				if p.err != nil {
					return fmt.Errorf("hint: %v, hash: %v, err: %w", hint, h.String(), p.err)
				}
				return nil
			}
		})
	}

	return eg.Wait().ErrorOrNil()
}

// GetMalfeasanceProofs gets malfeasance proofs for the specified NodeIDs and validates them.
func (f *Fetch) GetMalfeasanceProofs(ctx context.Context, ids []types.NodeID) error {
	if len(ids) == 0 {
		return nil
	}
	f.logger.WithContext(ctx).With().Debug("requesting malfeasance proofs from peer", log.Int("num_proofs", len(ids)))
	hashes := types.NodeIDsToHashes(ids)
	return f.getHashes(ctx, hashes, datastore.Malfeasance, f.validators.malfeasance.HandleMessage)
}

// GetBallots gets data for the specified BallotIDs and validates them.
func (f *Fetch) GetBallots(ctx context.Context, ids []types.BallotID) error {
	if len(ids) == 0 {
		return nil
	}
	f.logger.WithContext(ctx).With().Debug("requesting ballots from peer", log.Int("num_ballots", len(ids)))
	hashes := types.BallotIDsToHashes(ids)
	return f.getHashes(ctx, hashes, datastore.BallotDB, f.validators.ballot.HandleMessage)
}

// GetProposals gets the data for given proposal IDs from peers.
func (f *Fetch) GetProposals(ctx context.Context, ids []types.ProposalID) error {
	if len(ids) == 0 {
		return nil
	}
	f.logger.WithContext(ctx).With().Debug("requesting proposals from peer", log.Int("num_proposals", len(ids)))
	hashes := types.ProposalIDsToHashes(ids)
	return f.getHashes(ctx, hashes, datastore.ProposalDB, f.validators.proposal.HandleMessage)
}

// GetBlocks gets the data for given block IDs from peers.
func (f *Fetch) GetBlocks(ctx context.Context, ids []types.BlockID) error {
	if len(ids) == 0 {
		return nil
	}
	f.logger.WithContext(ctx).With().Debug("requesting blocks from peer", log.Int("num_blocks", len(ids)))
	hashes := types.BlockIDsToHashes(ids)
	return f.getHashes(ctx, hashes, datastore.BlockDB, f.validators.block.HandleMessage)
}

// GetProposalTxs fetches the txs provided as IDs and validates them, returns an error if one TX failed to be fetched.
func (f *Fetch) GetProposalTxs(ctx context.Context, ids []types.TransactionID) error {
	return f.getTxs(ctx, ids, f.validators.txProposal.HandleMessage)
}

// GetBlockTxs fetches the txs provided as IDs and saves them, they will be validated
// before block is applied.
func (f *Fetch) GetBlockTxs(ctx context.Context, ids []types.TransactionID) error {
	return f.getTxs(ctx, ids, f.validators.txBlock.HandleMessage)
}

func (f *Fetch) getTxs(ctx context.Context, ids []types.TransactionID, receiver dataReceiver) error {
	if len(ids) == 0 {
		return nil
	}
	f.logger.WithContext(ctx).With().Debug("requesting txs from peer", log.Int("num_txs", len(ids)))
	hashes := types.TransactionIDsToHashes(ids)
	return f.getHashes(ctx, hashes, datastore.TXDB, receiver)
}

// GetPoetProof gets poet proof from remote peer.
func (f *Fetch) GetPoetProof(ctx context.Context, id types.Hash32) error {
	f.logger.WithContext(ctx).With().Debug("getting poet proof", log.Stringer("hash", id))
	pm, err := f.getHash(ctx, id, datastore.POETDB, f.validators.poet.HandleMessage)
	if err != nil {
		return err
	}
	if pm == nil {
		// data is available locally
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-pm.completed:
	}
	switch {
	case pm.err == nil:
		return nil
	case errors.Is(pm.err, activation.ErrObjectExists):
		// PoET proofs are concurrently stored in DB in two places:
		// fetcher and nipost builder. Hence it might happen that
		// a proof had been inserted into the DB while the fetcher
		// was fetching.
		return nil
	default:
		f.logger.WithContext(ctx).With().Warning("failed to get hash",
			log.String("hint", string(datastore.POETDB)),
			log.Stringer("hash", id),
			log.Err(pm.err))
		return pm.err
	}
}

func (f *Fetch) GetMaliciousIDs(ctx context.Context, peers []p2p.Peer, okCB func([]byte, p2p.Peer), errCB func(error, p2p.Peer)) error {
	return poll(ctx, f.servers[malProtocol], peers, []byte{}, okCB, errCB)
}

// GetLayerData get layer data from peers.
func (f *Fetch) GetLayerData(ctx context.Context, peers []p2p.Peer, lid types.LayerID, okCB func([]byte, p2p.Peer), errCB func(error, p2p.Peer)) error {
	lidBytes, err := codec.Encode(&lid)
	if err != nil {
		return err
	}
	return poll(ctx, f.servers[lyrDataProtocol], peers, lidBytes, okCB, errCB)
}

// GetLayerOpinions get opinions on data in the specified layer from peers.
func (f *Fetch) GetLayerOpinions(ctx context.Context, peers []p2p.Peer, lid types.LayerID, okCB func([]byte, p2p.Peer), errCB func(error, p2p.Peer)) error {
	lidBytes, err := codec.Encode(&lid)
	if err != nil {
		return err
	}
	return poll(ctx, f.servers[lyrOpnsProtocol], peers, lidBytes, okCB, errCB)
}

func poll(ctx context.Context, srv requester, peers []p2p.Peer, req []byte, okCB func([]byte, p2p.Peer), errCB func(error, p2p.Peer)) error {
	for _, p := range peers {
		peer := p
		okFunc := func(data []byte) {
			okCB(data, peer)
		}
		errFunc := func(err error) {
			errCB(err, peer)
		}
		if err := srv.Request(ctx, peer, req, okFunc, errFunc); err != nil {
			errFunc(err)
		}
	}
	return nil
}

// PeerEpochInfo get the epoch info published in the given epoch from the specified peer.
func (f *Fetch) PeerEpochInfo(ctx context.Context, peer p2p.Peer, epoch types.EpochID) (*EpochData, error) {
	f.logger.WithContext(ctx).With().Debug("requesting epoch info from peer",
		log.Stringer("peer", peer),
		log.Stringer("epoch", epoch))

	var (
		done = make(chan error, 1)
		ed   EpochData
	)
	okCB := func(data []byte) {
		defer close(done)
		done <- codec.Decode(data, &ed)
	}
	errCB := func(perr error) {
		defer close(done)
		done <- perr
	}
	epochBytes, err := codec.Encode(epoch)
	if err != nil {
		return nil, err
	}
	if err := f.servers[atxProtocol].Request(ctx, peer, epochBytes, okCB, errCB); err != nil {
		return nil, err
	}
	select {
	case err := <-done:
		if err != nil {
			return nil, err
		}
		f.RegisterPeerHashes(peer, types.ATXIDsToHashes(ed.AtxIDs))
		return &ed, nil
	case <-ctx.Done():
		f.logger.WithContext(ctx).With().Debug("context done")
		return nil, ctx.Err()
	}
}

func iterateLayers(req *MeshHashRequest) ([]types.LayerID, error) {
	// TODO(mafa): there is MaxHashesInReq config in syncer that could be passed here instead of hardcoding this value
	if req.Steps == 0 || req.Steps > 100 {
		return nil, fmt.Errorf("%w: %v", errBadRequest, req)
	}

	// check for overflow
	if req.Delta == 0 || (req.Delta*req.Steps)/req.Delta != req.Steps {
		return nil, fmt.Errorf("%w: %v", errBadRequest, req)
	}

	var diff uint32
	if req.To.After(req.From) {
		diff = req.To.Difference(req.From)
	}
	if diff == 0 || diff > req.Delta*req.Steps || diff < req.Delta*(req.Steps-1) {
		return nil, fmt.Errorf("%w: %v", errBadRequest, req)
	}

	lids := make([]types.LayerID, req.Steps+1)
	lids[0] = req.From
	for i := uint32(1); i <= req.Steps; i++ {
		lids[i] = lids[i-1].Add(req.Delta)
	}
	if lids[req.Steps].After(req.To) {
		lids[req.Steps] = req.To
	}
	return lids, nil
}

func (f *Fetch) PeerMeshHashes(ctx context.Context, peer p2p.Peer, req *MeshHashRequest) (*MeshHashes, error) {
	f.logger.WithContext(ctx).With().Debug("requesting mesh hashes from peer",
		log.Stringer("peer", peer),
		log.Object("req", req))

	var (
		done    = make(chan error, 1)
		hashes  []types.Hash32
		reqData []byte
	)
	reqData, err := codec.Encode(req)
	if err != nil {
		f.logger.Fatal("failed to encode mesh hash request", log.Err(err))
	}
	lids, err := iterateLayers(req)
	if err != nil {
		return nil, err
	}

	okCB := func(data []byte) {
		defer close(done)
		h, err := codec.DecodeSlice[types.Hash32](data)
		hashes = h
		done <- err
	}
	errCB := func(perr error) {
		defer close(done)
		done <- perr
	}
	if err = f.servers[meshHashProtocol].Request(ctx, peer, reqData, okCB, errCB); err != nil {
		return nil, err
	}
	select {
	case err := <-done:
		if err != nil {
			return nil, err
		}
		return &MeshHashes{
			Layers: lids,
			Hashes: hashes,
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
