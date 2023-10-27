package fetch

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"

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

type dataReceiver func(context.Context, types.Hash32, p2p.Peer, []byte) error

func (f *Fetch) getHashes(
	ctx context.Context,
	hashes []types.Hash32,
	hint datastore.Hint,
	receiver dataReceiver,
) error {
	var eg errgroup.Group
	var errs error
	var mu sync.Mutex
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
					mu.Lock()
					defer mu.Unlock()
					err := fmt.Errorf("hint: %v, hash: %v, err: %w", hint, h.String(), p.err)
					errs = errors.Join(errs, err)
				}
				return nil
			}
		})
	}

	err := eg.Wait()
	return errors.Join(errs, err)
}

// GetActiveSet downloads activeset.
func (f *Fetch) GetActiveSet(ctx context.Context, set types.Hash32) error {
	f.logger.WithContext(ctx).With().Debug("request active set", log.ShortStringer("id", set))
	return f.getHashes(ctx, []types.Hash32{set}, datastore.ActiveSet, f.validators.activeset.HandleMessage)
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

func (f *Fetch) GetMaliciousIDs(
	ctx context.Context,
	peers []p2p.Peer,
	okCB func([]byte, p2p.Peer),
	errCB func(error, p2p.Peer),
) error {
	return poll(ctx, f.servers[malProtocol], peers, []byte{}, okCB, errCB)
}

// GetLayerData get layer data from peers.
func (f *Fetch) GetLayerData(
	ctx context.Context,
	peers []p2p.Peer,
	lid types.LayerID,
	okCB func([]byte, p2p.Peer),
	errCB func(error, p2p.Peer),
) error {
	lidBytes, err := codec.Encode(&lid)
	if err != nil {
		return err
	}
	return poll(ctx, f.servers[lyrDataProtocol], peers, lidBytes, okCB, errCB)
}

func (f *Fetch) GetLayerOpinions(
	ctx context.Context,
	peers []p2p.Peer,
	lid types.LayerID,
	okCB func([]byte, p2p.Peer),
	errCB func(error, p2p.Peer),
) error {
	req := OpinionRequest{
		Layer: lid,
	}
	reqData, err := codec.Encode(&req)
	if err != nil {
		return err
	}
	return poll(ctx, f.servers[OpnProtocol], peers, reqData, okCB, errCB)
}

func poll(
	ctx context.Context,
	srv requester,
	peers []p2p.Peer,
	req []byte,
	okCB func([]byte, p2p.Peer),
	errCB func(error, p2p.Peer),
) error {
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
		done <- codec.Decode(data, &ed)
	}
	errCB := func(perr error) {
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

func (f *Fetch) PeerMeshHashes(ctx context.Context, peer p2p.Peer, req *MeshHashRequest) (*MeshHashes, error) {
	f.logger.WithContext(ctx).With().Debug("requesting mesh hashes from peer",
		log.Stringer("peer", peer),
		log.Object("req", req),
	)

	var (
		done    = make(chan error, 1)
		hashes  []types.Hash32
		reqData []byte
	)
	reqData, err := codec.Encode(req)
	if err != nil {
		f.logger.With().Fatal("failed to encode mesh hash request", log.Err(err))
	}

	okCB := func(data []byte) {
		h, err := codec.DecodeSlice[types.Hash32](data)
		hashes = h
		done <- err
	}
	errCB := func(perr error) {
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
			Hashes: hashes,
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *Fetch) GetCert(
	ctx context.Context,
	lid types.LayerID,
	bid types.BlockID,
	peers []p2p.Peer,
) (*types.Certificate, error) {
	f.logger.WithContext(ctx).With().Debug("requesting block certificate from peers",
		lid, bid, log.Int("num peer", len(peers)))
	req := &OpinionRequest{
		Layer: lid,
		Block: &bid,
	}
	reqData, err := codec.Encode(req)
	if err != nil {
		f.logger.With().Fatal("failed to encode cert request", log.Err(err))
	}

	out := make(chan *types.Certificate, 1)
	for _, peer := range peers {
		done := make(chan error, 1)
		okCB := func(data []byte) {
			var peerCert types.Certificate
			if err = codec.Decode(data, &peerCert); err != nil {
				done <- err
				return
			}
			// for generic data fetches by hash (ID for atx/block/proposal/ballot/tx), the check on whether the returned
			// data matching the hash was done on the data handlers' path. for block certificate, there is no ID associated
			// with it, hence the check here.
			// however, certificate doesn't go through that path. it's requested by a separate protocol because a block
			// certificate doesn't have an ID.
			if peerCert.BlockID != bid {
				done <- fmt.Errorf("peer %v served wrong cert. want %s got %s", peer, bid.String(), peerCert.BlockID.String())
				return
			}
			out <- &peerCert
		}
		errCB := func(perr error) {
			done <- perr
		}
		if err := f.servers[OpnProtocol].Request(ctx, peer, reqData, okCB, errCB); err != nil {
			done <- err
		}
		select {
		case err := <-done:
			f.logger.With().Debug("failed to get cert from peer",
				log.Stringer("peer", peer),
				log.Err(err),
			)
			continue
		case cert := <-out:
			return cert, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("failed to get cert %v/%s from %d peers", lid, bid.String(), len(peers))
}
