package fetch

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/system"
)

var errBadRequest = errors.New("invalid request")

// GetAtxs gets the data for given atx IDs and validates them. returns an error if at least one ATX cannot be fetched.
func (f *Fetch) GetAtxs(ctx context.Context, ids []types.ATXID, opts ...system.GetAtxOpt) error {
	if len(ids) == 0 {
		return nil
	}

	options := system.GetAtxOpts{}
	for _, opt := range opts {
		opt(&options)
	}

	f.logger.WithContext(ctx).With().
		Debug("requesting atxs from peer", log.Int("num_atxs", len(ids)), log.Bool("limiting", !options.LimitingOff))
	hashes := types.ATXIDsToHashes(ids)
	if options.LimitingOff {
		return f.getHashes(ctx, hashes, datastore.ATXDB, f.validators.atx.HandleMessage)
	}
	return f.getHashes(ctx, hashes, datastore.ATXDB, f.validators.atx.HandleMessage, withLimiter(f.getAtxsLimiter))
}

type dataReceiver func(context.Context, types.Hash32, p2p.Peer, []byte) error

type getHashesOpt func(*getHashesOpts)

func withLimiter(l limiter) getHashesOpt {
	return func(o *getHashesOpts) {
		o.limiter = l
	}
}

func (f *Fetch) getHashes(
	ctx context.Context,
	hashes []types.Hash32,
	hint datastore.Hint,
	receiver dataReceiver,
	opts ...getHashesOpt,
) error {
	options := getHashesOpts{
		limiter: noLimit{},
	}
	for _, opt := range opts {
		opt(&options)
	}

	pendingMetric := pendingHashReqs.WithLabelValues(string(hint))
	pendingMetric.Add(float64(len(hashes)))

	var eg errgroup.Group
	var failed atomic.Uint64
	for i, hash := range hashes {
		if err := options.limiter.Acquire(ctx, 1); err != nil {
			pendingMetric.Add(float64(i - len(hashes)))
			return fmt.Errorf("acquiring slot to get hash: %w", err)
		}
		p, err := f.getHash(ctx, hash, hint, receiver)
		if err != nil {
			options.limiter.Release(1)
			pendingMetric.Add(float64(i - len(hashes)))
			return err
		}
		if p == nil {
			// data is available locally
			options.limiter.Release(1)
			pendingMetric.Add(-1)
			continue
		}

		h := hash
		eg.Go(func() error {
			select {
			case <-ctx.Done():
				options.limiter.Release(1)
				pendingMetric.Add(-1)
				return ctx.Err()
			case <-p.completed:
				options.limiter.Release(1)
				pendingMetric.Add(-1)
				if p.err != nil {
					f.logger.Debug("failed to get hash",
						log.String("hint", string(hint)),
						log.Stringer("hash", h),
						log.Err(p.err),
					)
					failed.Add(1)
				}
				return nil
			}
		})
	}

	eg.Wait()
	if failed.Load() > 0 {
		return fmt.Errorf("failed to fetch %d hashes out of %d", failed.Load(), len(hashes))
	}
	return nil
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
		// fetcher and nipost builder. Hence, it might happen that
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

func (f *Fetch) GetMaliciousIDs(ctx context.Context, peer p2p.Peer) ([]byte, error) {
	return f.servers[malProtocol].Request(ctx, peer, []byte{})
}

// GetLayerData get layer data from peers.
func (f *Fetch) GetLayerData(ctx context.Context, peer p2p.Peer, lid types.LayerID) ([]byte, error) {
	lidBytes, err := codec.Encode(&lid)
	if err != nil {
		return nil, err
	}
	return f.servers[lyrDataProtocol].Request(ctx, peer, lidBytes)
}

func (f *Fetch) GetLayerOpinions(ctx context.Context, peer p2p.Peer, lid types.LayerID) ([]byte, error) {
	req := OpinionRequest{
		Layer: lid,
	}
	reqData, err := codec.Encode(&req)
	if err != nil {
		return nil, err
	}
	return f.servers[OpnProtocol].Request(ctx, peer, reqData)
}

// PeerEpochInfo get the epoch info published in the given epoch from the specified peer.
func (f *Fetch) PeerEpochInfo(ctx context.Context, peer p2p.Peer, epoch types.EpochID) (*EpochData, error) {
	f.logger.WithContext(ctx).With().Debug("requesting epoch info from peer",
		log.Stringer("peer", peer),
		log.Stringer("epoch", epoch))

	epochBytes, err := codec.Encode(epoch)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	data, err := f.servers[atxProtocol].Request(ctx, peer, epochBytes)
	if err != nil {
		f.peers.OnFailure(peer)
		return nil, err
	}
	f.peers.OnLatency(peer, len(data), time.Since(start))

	var ed EpochData
	if err := codec.Decode(data, &ed); err != nil {
		return nil, fmt.Errorf("decoding epoch data: %w", err)
	}
	f.RegisterPeerHashes(peer, types.ATXIDsToHashes(ed.AtxIDs))
	return &ed, nil
}

func (f *Fetch) PeerMeshHashes(ctx context.Context, peer p2p.Peer, req *MeshHashRequest) (*MeshHashes, error) {
	f.logger.WithContext(ctx).With().Debug("requesting mesh hashes from peer",
		log.Stringer("peer", peer),
		log.Object("req", req),
	)

	reqData, err := codec.Encode(req)
	if err != nil {
		f.logger.With().Fatal("failed to encode mesh hash request", log.Err(err))
	}

	data, err := f.servers[meshHashProtocol].Request(ctx, peer, reqData)
	if err != nil {
		return nil, err
	}
	hashes, err := codec.DecodeSlice[types.Hash32](data)
	if err != nil {
		return nil, fmt.Errorf("decoding hashes response: %w", err)
	}
	return &MeshHashes{
		Hashes: hashes,
	}, nil
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
	reqData := codec.MustEncode(req)

	for _, peer := range peers {
		data, err := f.servers[OpnProtocol].Request(ctx, peer, reqData)
		if err != nil {
			f.logger.With().Debug("failed to get cert", log.Stringer("peer", peer), log.Err(err))
			continue
		}
		var peerCert types.Certificate
		if err = codec.Decode(data, &peerCert); err != nil {
			f.logger.With().Debug("failed to decode cert", log.Stringer("peer", peer), log.Err(err))
			continue
		}
		// for generic data fetches by hash (ID for atx/block/proposal/ballot/tx), the check on whether the returned
		// data matching the hash was done on the data handlers' path. for block certificate, there is no ID associated
		// with it, hence the check here.
		// however, certificate doesn't go through that path. it's requested by a separate protocol because a block
		// certificate doesn't have an ID.
		if peerCert.BlockID != bid {
			f.logger.With().Debug(
				"peer served wrong cert",
				log.Stringer("want", bid),
				log.Stringer("got", peerCert.BlockID),
				log.Stringer("peer", peer),
			)
		}
		return &peerCert, nil
	}
	return nil, fmt.Errorf("failed to get cert %v/%s from %d peers: %w", lid, bid.String(), len(peers), ctx.Err())
}
