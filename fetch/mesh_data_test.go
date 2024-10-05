package fetch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	p2phost "github.com/libp2p/go-libp2p/core/host"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/fetch/mocks"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/peerinfo"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/proposals/store"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/vm/sdk/wallet"
)

const (
	txsForBlock    = iota
	txsForProposal = iota
)

type hostWrapper struct {
	p2phost.Host
}

func (hw *hostWrapper) PeerInfo() peerinfo.PeerInfo { return nil }

func wrapHost(h p2phost.Host) server.Host {
	return &hostWrapper{Host: h}
}

func (f *testFetch) withMethod(method int) *testFetch {
	f.method = method
	return f
}

func (f *testFetch) expectTransactionCall(times int) *gomock.Call {
	if f.method == txsForBlock {
		return f.mTxBlocksH.EXPECT().
			HandleMessage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Times(times)
	} else if f.method == txsForProposal {
		return f.mTxProposalH.EXPECT().HandleMessage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Times(times)
	}
	return nil
}

func (f *testFetch) testGetTxs(tids []types.TransactionID) error {
	if f.method == txsForBlock {
		return f.GetBlockTxs(context.Background(), tids)
	} else if f.method == txsForProposal {
		return f.GetProposalTxs(context.Background(), tids)
	}
	return nil
}

const (
	numBallots   = 10
	numMalicious = 11
)

func startTestLoop(t *testing.T, f *Fetch, eg *errgroup.Group, stop chan struct{}) {
	t.Helper()
	eg.Go(func() error {
		for {
			select {
			case <-stop:
				return nil
			default:
				f.mu.Lock()
				for h, req := range f.unprocessed {
					require.NoError(t, req.validator(req.ctx, types.Hash32{}, p2p.NoPeer, []byte{}))
					close(req.promise.completed)
					delete(f.unprocessed, h)
				}
				f.mu.Unlock()
			}
		}
	})
}

func generateMaliciousIDs(t *testing.T) []types.NodeID {
	t.Helper()
	malIDs := make([]types.NodeID, numMalicious)
	for i := range malIDs {
		malIDs[i] = types.RandomNodeID()
	}
	return malIDs
}

func generateLayerContent(t *testing.T) []byte {
	t.Helper()
	ballotIDs := make([]types.BallotID, 0, numBallots)
	for i := 0; i < numBallots; i++ {
		ballotIDs = append(ballotIDs, types.RandomBallotID())
	}
	lb := LayerData{
		Ballots: ballotIDs,
	}
	out, _ := codec.Encode(&lb)
	return out
}

func TestFetch_getHashes(t *testing.T) {
	blks := []*types.Block{
		genLayerBlock(types.LayerID(10), types.RandomTXSet(10)),
		genLayerBlock(types.LayerID(11), types.RandomTXSet(10)),
		genLayerBlock(types.LayerID(20), types.RandomTXSet(10)),
	}
	blockIDs := types.ToBlockIDs(blks)
	hashes := types.BlockIDsToHashes(blockIDs)
	peers := []p2p.Peer{p2p.Peer("buddy 0"), p2p.Peer("buddy 1")}

	newFetcher := func(t testing.TB) *testFetch {
		f := createFetch(t)
		f.cfg.QueueSize = 3
		f.cfg.BatchSize = 2
		f.cfg.MaxRetriesForRequest = 0
		f.cfg.Streaming = false
		f.Start()
		t.Cleanup(f.Stop)
		for _, peer := range peers {
			f.peers.Add(peer)
		}
		f.mh.EXPECT().ID().Return("self").AnyTimes()
		f.RegisterPeerHashes(peers[0], hashes[:2])
		f.RegisterPeerHashes(peers[1], hashes[2:])
		return f
	}

	t.Run("all hashes fetched", func(t *testing.T) {
		t.Parallel()
		f := newFetcher(t)
		requestFn := func(_ context.Context, p p2p.Peer, req []byte, _ ...string) ([]byte, error) {
			var rb RequestBatch
			codec.MustDecode(req, &rb)
			resBatch := ResponseBatch{ID: rb.ID}
			for _, r := range rb.Requests {
				res := ResponseMessage{Hash: r.Hash, Data: []byte("a")}
				resBatch.Responses = append(resBatch.Responses, res)
				f.mBlocksH.EXPECT().HandleMessage(gomock.Any(), res.Hash, p, res.Data)
			}
			return codec.MustEncode(&resBatch), nil
		}
		f.mHashS.EXPECT().Request(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(requestFn).Times(len(peers))
		err := f.getHashes(context.Background(), hashes, datastore.BlockDB, f.validators.block.HandleMessage)
		require.NoError(t, err)
	})

	t.Run("all hashes failed", func(t *testing.T) {
		t.Parallel()
		f := newFetcher(t)
		requestFn := func(_ context.Context, _ p2p.Peer, req []byte, _ ...string) ([]byte, error) {
			var rb RequestBatch
			codec.MustDecode(req, &rb)
			resBatch := ResponseBatch{ID: rb.ID}
			return codec.MustEncode(&resBatch), nil
		}
		f.mHashS.EXPECT().Request(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(requestFn).Times(len(peers))
		err := f.getHashes(context.Background(), hashes, datastore.BlockDB, f.validators.block.HandleMessage)
		require.Error(t, err)
	})

	t.Run("hash request failed", func(t *testing.T) {
		t.Parallel()
		f := newFetcher(t)
		f.mHashS.EXPECT().Request(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, errors.New("request failed")).
			Times(len(peers))
		err := f.getHashes(context.Background(), hashes, datastore.BlockDB, f.validators.block.HandleMessage)
		require.Error(t, err)
	})

	t.Run("some hashes failed", func(t *testing.T) {
		t.Parallel()
		f := newFetcher(t)
		requestFn := func(_ context.Context, p p2p.Peer, req []byte, _ ...string) ([]byte, error) {
			var rb RequestBatch
			codec.MustDecode(req, &rb)
			resBatch := ResponseBatch{ID: rb.ID}
			for _, r := range rb.Requests {
				if r.Hash == hashes[1] {
					continue
				}
				res := ResponseMessage{Hash: r.Hash, Data: []byte("a")}
				resBatch.Responses = append(resBatch.Responses, res)
				f.mBlocksH.EXPECT().HandleMessage(gomock.Any(), res.Hash, p, res.Data).Return(nil)
			}
			return codec.MustEncode(&resBatch), nil
		}
		f.mHashS.EXPECT().Request(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(requestFn).Times(len(peers))
		err := f.getHashes(context.Background(), hashes, datastore.BlockDB, f.validators.block.HandleMessage)
		require.Error(t, err)
	})

	t.Run("handler failed", func(t *testing.T) {
		t.Parallel()
		f := newFetcher(t)
		requestFn := func(_ context.Context, p p2p.Peer, req []byte, _ ...string) ([]byte, error) {
			var rb RequestBatch
			codec.MustDecode(req, &rb)
			resBatch := ResponseBatch{ID: rb.ID}
			for _, r := range rb.Requests {
				res := ResponseMessage{Hash: r.Hash, Data: []byte("a")}
				resBatch.Responses = append(resBatch.Responses, res)
				f.mBlocksH.EXPECT().HandleMessage(gomock.Any(), res.Hash, p, res.Data).Return(errors.New("unknown"))
			}
			return codec.MustEncode(&resBatch), nil
		}
		f.mHashS.EXPECT().Request(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(requestFn).Times(len(peers))
		err := f.getHashes(context.Background(), hashes, datastore.BlockDB, f.validators.block.HandleMessage)
		require.Error(t, err)
	})
}

func TestFetch_getHashesStreaming(t *testing.T) {
	blks := []*types.Block{
		genLayerBlock(types.LayerID(10), types.RandomTXSet(10)),
		genLayerBlock(types.LayerID(11), types.RandomTXSet(10)),
		genLayerBlock(types.LayerID(20), types.RandomTXSet(10)),
	}
	blockIDs := types.ToBlockIDs(blks)
	hashes := types.BlockIDsToHashes(blockIDs)
	peers := []p2p.Peer{p2p.Peer("buddy 0"), p2p.Peer("buddy 1")}

	newFetcher := func(t testing.TB) *testFetch {
		f := createFetch(t)
		f.cfg.QueueSize = 3
		f.cfg.BatchSize = 2
		f.cfg.MaxRetriesForRequest = 0
		f.cfg.Streaming = true
		f.Start()
		t.Cleanup(f.Stop)
		for _, peer := range peers {
			f.peers.Add(peer)
		}
		f.mh.EXPECT().ID().Return("self").AnyTimes()
		f.RegisterPeerHashes(peers[0], hashes[:2])
		f.RegisterPeerHashes(peers[1], hashes[2:])
		return f
	}

	t.Run("all hashes fetched", func(t *testing.T) {
		t.Parallel()
		f := newFetcher(t)
		streamFn := func(
			ctx context.Context,
			p p2p.Peer,
			req []byte,
			cbk server.StreamRequestCallback,
			_ ...string,
		) error {
			var rb RequestBatch
			codec.MustDecode(req, &rb)
			resBatch := ResponseBatch{ID: rb.ID}
			for _, r := range rb.Requests {
				res := ResponseMessage{Hash: r.Hash, Data: []byte("a")}
				resBatch.Responses = append(resBatch.Responses, res)
				f.mBlocksH.EXPECT().HandleMessage(gomock.Any(), res.Hash, p, res.Data)
			}
			buf := bytes.NewBuffer(codec.MustEncode(&server.Response{Data: codec.MustEncode(&resBatch)}))
			return cbk(ctx, buf)
		}
		f.mHashS.EXPECT().StreamRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(streamFn).
			Times(len(peers))
		err := f.getHashes(context.Background(), hashes, datastore.BlockDB, f.validators.block.HandleMessage)
		require.NoError(t, err)
	})

	t.Run("all hashes failed", func(t *testing.T) {
		t.Parallel()
		f := newFetcher(t)
		streamFn := func(
			ctx context.Context,
			_ p2p.Peer,
			req []byte,
			cbk server.StreamRequestCallback,
			_ ...string,
		) error {
			var rb RequestBatch
			codec.MustDecode(req, &rb)
			resBatch := ResponseBatch{ID: rb.ID}
			buf := bytes.NewBuffer(codec.MustEncode(&server.Response{Data: codec.MustEncode(&resBatch)}))
			return cbk(ctx, buf)
		}
		f.mHashS.EXPECT().StreamRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(streamFn).
			Times(len(peers))
		err := f.getHashes(context.Background(), hashes, datastore.BlockDB, f.validators.block.HandleMessage)
		require.Error(t, err)
	})

	t.Run("hash request failed", func(t *testing.T) {
		t.Parallel()
		f := newFetcher(t)
		f.mHashS.EXPECT().StreamRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.New("request failed")).
			Times(len(peers))
		err := f.getHashes(context.Background(), hashes, datastore.BlockDB, f.validators.block.HandleMessage)
		require.Error(t, err)
	})

	t.Run("some hashes failed", func(t *testing.T) {
		t.Parallel()
		f := newFetcher(t)
		streamFn := func(ctx context.Context,
			p p2p.Peer,
			req []byte,
			cbk server.StreamRequestCallback,
			_ ...string,
		) error {
			var rb RequestBatch
			codec.MustDecode(req, &rb)
			resBatch := ResponseBatch{ID: rb.ID}
			for _, r := range rb.Requests {
				if r.Hash == hashes[1] {
					continue
				}
				res := ResponseMessage{Hash: r.Hash, Data: []byte("a")}
				resBatch.Responses = append(resBatch.Responses, res)
				f.mBlocksH.EXPECT().HandleMessage(gomock.Any(), res.Hash, p, res.Data)
			}
			buf := bytes.NewBuffer(codec.MustEncode(&server.Response{Data: codec.MustEncode(&resBatch)}))
			return cbk(ctx, buf)
		}
		f.mHashS.EXPECT().StreamRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(streamFn).
			Times(len(peers))
		err := f.getHashes(context.Background(), hashes, datastore.BlockDB, f.validators.block.HandleMessage)
		require.Error(t, err)
	})

	t.Run("handler failed", func(t *testing.T) {
		t.Parallel()
		f := newFetcher(t)
		streamFn := func(ctx context.Context,
			p p2p.Peer,
			req []byte,
			cbk server.StreamRequestCallback,
			_ ...string,
		) error {
			var rb RequestBatch
			codec.MustDecode(req, &rb)
			resBatch := ResponseBatch{ID: rb.ID}
			for _, r := range rb.Requests {
				res := ResponseMessage{Hash: r.Hash, Data: []byte("a")}
				resBatch.Responses = append(resBatch.Responses, res)
				f.mBlocksH.EXPECT().HandleMessage(gomock.Any(), res.Hash, p, res.Data).Return(errors.New("unknown"))
			}
			buf := bytes.NewBuffer(codec.MustEncode(&server.Response{Data: codec.MustEncode(&resBatch)}))
			return cbk(ctx, buf)
		}
		f.mHashS.EXPECT().StreamRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(streamFn).
			Times(len(peers))
		err := f.getHashes(context.Background(), hashes, datastore.BlockDB, f.validators.block.HandleMessage)
		require.Error(t, err)
	})
}

func TestFetch_GetMalfeasanceProofs(t *testing.T) {
	nodeIDs := []types.NodeID{{1}, {2}, {3}}
	f := createFetch(t)
	f.mMalH.EXPECT().
		HandleMessage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).
		Times(len(nodeIDs))

	stop := make(chan struct{}, 1)
	var eg errgroup.Group
	startTestLoop(t, f.Fetch, &eg, stop)

	require.NoError(t, f.GetMalfeasanceProofs(context.Background(), nodeIDs))
	close(stop)
	require.NoError(t, eg.Wait())
}

func TestFetch_GetBlocks(t *testing.T) {
	blks := []*types.Block{
		genLayerBlock(types.LayerID(10), types.RandomTXSet(10)),
		genLayerBlock(types.LayerID(20), types.RandomTXSet(10)),
	}
	blockIDs := types.ToBlockIDs(blks)
	f := createFetch(t)
	f.mBlocksH.EXPECT().
		HandleMessage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).
		Times(len(blockIDs))

	stop := make(chan struct{}, 1)
	var eg errgroup.Group
	startTestLoop(t, f.Fetch, &eg, stop)

	require.NoError(t, f.GetBlocks(context.Background(), blockIDs))
	close(stop)
	require.NoError(t, eg.Wait())
}

func TestFetch_GetBallots(t *testing.T) {
	blts := []*types.Ballot{
		genLayerBallot(t, types.LayerID(10)),
		genLayerBallot(t, types.LayerID(20)),
	}
	ballotIDs := types.ToBallotIDs(blts)
	f := createFetch(t)
	f.mBallotH.EXPECT().
		HandleMessage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).
		Times(len(ballotIDs))

	stop := make(chan struct{}, 1)
	var eg errgroup.Group
	startTestLoop(t, f.Fetch, &eg, stop)

	require.NoError(t, f.GetBallots(context.Background(), ballotIDs))
	close(stop)
	require.NoError(t, eg.Wait())
}

func genLayerProposal(
	tb testing.TB,
	layerID types.LayerID,
	txs []types.TransactionID,
) *types.Proposal {
	tb.Helper()
	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: types.Ballot{
				InnerBallot: types.InnerBallot{
					Layer: layerID,
					AtxID: types.RandomATXID(),
					EpochData: &types.EpochData{
						Beacon: types.RandomBeacon(),
					},
				},
			},
			TxIDs: txs,
		},
	}
	signer, err := signing.NewEdSigner()
	require.NoError(tb, err)
	p.Ballot.Signature = signer.Sign(signing.BALLOT, p.Ballot.SignedBytes())
	p.Signature = signer.Sign(signing.PROPOSAL, p.SignedBytes())
	p.SmesherID = signer.NodeID()
	p.Initialize()
	return p
}

func genLayerBallot(tb testing.TB, layerID types.LayerID) *types.Ballot {
	b := types.RandomBallot()
	b.Layer = layerID
	signer, err := signing.NewEdSigner()
	require.NoError(tb, err)
	b.Signature = signer.Sign(signing.BALLOT, b.SignedBytes())
	b.SmesherID = signer.NodeID()
	b.Initialize()
	return b
}

func genLayerBlock(layerID types.LayerID, txs []types.TransactionID) *types.Block {
	b := &types.Block{
		InnerBlock: types.InnerBlock{
			LayerIndex: layerID,
			TxIDs:      txs,
		},
	}
	b.Initialize()
	return b
}

func TestFetch_GetProposals(t *testing.T) {
	proposals := []*types.Proposal{
		genLayerProposal(t, types.LayerID(10), nil),
		genLayerProposal(t, types.LayerID(20), nil),
	}
	proposalIDs := types.ToProposalIDs(proposals)
	f := createFetch(t)
	f.mProposalH.EXPECT().
		HandleMessage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).
		Times(len(proposalIDs))

	stop := make(chan struct{}, 1)
	var eg errgroup.Group
	startTestLoop(t, f.Fetch, &eg, stop)

	require.NoError(t, f.GetProposals(context.Background(), proposalIDs))
	close(stop)
	require.NoError(t, eg.Wait())
}

func genTx(
	tb testing.TB,
	signer *signing.EdSigner,
	dest types.Address,
	amount, nonce, price uint64,
) types.Transaction {
	tb.Helper()
	raw := wallet.Spend(signer.PrivateKey(), dest, amount,
		nonce,
	)
	tx := types.Transaction{
		RawTx:    types.NewRawTx(raw),
		TxHeader: &types.TxHeader{},
	}
	tx.MaxGas = 100
	tx.MaxSpend = amount
	tx.GasPrice = price
	tx.Nonce = nonce
	tx.Principal = types.GenerateAddress(signer.PublicKey().Bytes())
	return tx
}

func genTransactions(tb testing.TB, num int) []*types.Transaction {
	tb.Helper()
	txs := make([]*types.Transaction, 0, num)
	for i := 0; i < num; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(tb, err)
		tx := genTx(tb, signer, types.Address{1}, 1, 1, 1)
		txs = append(txs, &tx)
	}
	return txs
}

func TestFetch_GetTxs(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		method int
	}{
		{
			desc:   "proposal",
			method: txsForProposal,
		},
		{
			desc:   "block",
			method: txsForBlock,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			f := createFetch(t).withMethod(tc.method)
			txs := genTransactions(t, 19)
			tids := types.ToTransactionIDs(txs)
			f.expectTransactionCall(len(tids))

			stop := make(chan struct{}, 1)
			var eg errgroup.Group
			startTestLoop(t, f.Fetch, &eg, stop)

			require.NoError(t, f.testGetTxs(tids))
			close(stop)
			require.NoError(t, eg.Wait())
		})
	}
}

func genATXs(tb testing.TB, num uint32) []*types.ActivationTx {
	tb.Helper()
	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)
	atxs := make([]*types.ActivationTx, 0, num)
	for i := uint32(0); i < num; i++ {
		atx := &types.ActivationTx{
			Coinbase: types.Address{1, 2, 3},
			NumUnits: i,
			Weight:   uint64(i),
		}
		atx.SmesherID = sig.NodeID()
		atx.SetID(types.RandomATXID())
		atxs = append(atxs, atx)
	}
	return atxs
}

func TestGetATXs(t *testing.T) {
	atxs := genATXs(t, 2)
	f := createFetch(t)
	f.mAtxH.EXPECT().
		HandleMessage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).
		Times(len(atxs))

	stop := make(chan struct{}, 1)
	var eg errgroup.Group
	startTestLoop(t, f.Fetch, &eg, stop)

	atxIDs := types.ToATXIDs(atxs)
	require.NoError(t, f.GetAtxs(context.Background(), atxIDs))
	close(stop)
	require.NoError(t, eg.Wait())
}

func TestGetActiveSet(t *testing.T) {
	f := createFetch(t)
	f.mActiveSetH.EXPECT().
		HandleMessage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil)

	stop := make(chan struct{}, 1)
	var eg errgroup.Group
	startTestLoop(t, f.Fetch, &eg, stop)

	require.NoError(t, f.GetActiveSet(context.Background(), types.Hash32{1, 2, 3}))
	close(stop)
	require.NoError(t, eg.Wait())
}

func TestGetPoetProof(t *testing.T) {
	f := createFetch(t)
	h := types.RandomHash()
	f.mPoetH.EXPECT().
		HandleMessage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil)

	stop := make(chan struct{}, 1)
	var eg errgroup.Group
	startTestLoop(t, f.Fetch, &eg, stop)

	require.NoError(t, f.GetPoetProof(context.Background(), h))
	close(stop)
	require.NoError(t, eg.Wait())
}

func TestFetch_GetMaliciousIDs(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		t.Parallel()
		f := createFetch(t)
		expectedIds := generateMaliciousIDs(t)
		resp := codec.MustEncode(&MaliciousIDs{NodeIDs: expectedIds})
		f.mh.EXPECT().ID().Return("self").AnyTimes()
		f.mMalS.EXPECT().Request(gomock.Any(), p2p.Peer("p0"), []byte{}).Return(resp, nil)
		ids, err := f.GetMaliciousIDs(context.Background(), "p0")
		require.NoError(t, err)
		require.Equal(t, expectedIds, ids)
	})
	t.Run("failure", func(t *testing.T) {
		t.Parallel()
		errUnknown := errors.New("unknown")
		f := createFetch(t)
		f.mMalS.EXPECT().Request(gomock.Any(), p2p.Peer("p0"), []byte{}).Return(nil, errUnknown)
		ids, err := f.GetMaliciousIDs(context.Background(), "p0")
		require.ErrorIs(t, err, errUnknown)
		require.Nil(t, ids)
	})
}

func TestFetch_GetLayerOpinions(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		t.Parallel()
		f := createFetch(t)
		expected := generateLayerContent(t)
		f.mOpn2S.EXPECT().Request(gomock.Any(), p2p.Peer("p0"), gomock.Any()).Return(expected, nil)
		res, err := f.GetLayerOpinions(context.Background(), "p0", 7)
		require.NoError(t, err)
		require.Equal(t, expected, res)
	})
	t.Run("failure", func(t *testing.T) {
		t.Parallel()
		errUnknown := errors.New("unknown")
		f := createFetch(t)
		f.mOpn2S.EXPECT().Request(gomock.Any(), p2p.Peer("p0"), gomock.Any()).Return(nil, errUnknown)
		res, err := f.GetLayerOpinions(context.Background(), "p0", 7)
		require.ErrorIs(t, err, errUnknown)
		require.Nil(t, res)
	})
}

func TestFetch_GetLayerData(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		t.Parallel()
		f := createFetch(t)
		expected := generateLayerContent(t)
		f.mLyrS.EXPECT().Request(gomock.Any(), p2p.Peer("p0"), gomock.Any()).Return(expected, nil)
		res, err := f.GetLayerData(context.Background(), "p0", 7)
		require.NoError(t, err)
		require.Equal(t, expected, res)
	})
	t.Run("failure", func(t *testing.T) {
		t.Parallel()
		errUnknown := errors.New("unknown")
		f := createFetch(t)
		f.mLyrS.EXPECT().Request(gomock.Any(), p2p.Peer("p0"), gomock.Any()).Return(nil, errUnknown)
		res, err := f.GetLayerData(context.Background(), "p0", 7)
		require.ErrorIs(t, err, errUnknown)
		require.Nil(t, res)
	})
}

func generateEpochData(t *testing.T) (*EpochData, []byte) {
	t.Helper()
	ed := &EpochData{
		AtxIDs: types.RandomActiveSet(11),
	}
	data, err := codec.Encode(ed)
	require.NoError(t, err)
	return ed, data
}

func Test_PeerEpochInfo(t *testing.T) {
	peer := p2p.Peer("p0")
	tt := []struct {
		name      string
		err       string
		streaming bool
		padding   int
	}{
		{
			name: "success",
		},
		{
			name: "fail",
			err:  "unknown",
		},
		{
			name:      "success (streamed)",
			streaming: true,
		},
		{
			name:      "fail (streamed)",
			err:       "unknown",
			streaming: true,
		},
		{
			name:      "fail (streamed, padding)",
			err:       "bad slice length",
			streaming: true,
			padding:   10,
		},
		{
			name:      "fail (streamed, truncated)",
			err:       "bad slice length",
			streaming: true,
			padding:   -1,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := createFetch(t)
			f.cfg.Streaming = tc.streaming
			f.mh.EXPECT().ID().Return("self").AnyTimes()
			var expected *EpochData
			epochIDBytes := codec.MustEncode(types.EpochID(111))
			if tc.streaming {
				f.mAtxS.EXPECT().
					StreamRequest(gomock.Any(), peer, epochIDBytes, gomock.Any()).
					DoAndReturn(
						func(
							ctx context.Context,
							_ p2p.Peer,
							_ []byte,
							cbk server.StreamRequestCallback,
							extraProtocols ...string,
						) error {
							if tc.err == "" || tc.padding != 0 {
								var r server.Response
								expected, r.Data = generateEpochData(t)
								if tc.padding > 0 {
									r.Data = append(r.Data, make([]byte, tc.padding)...)
								} else {
									r.Data = r.Data[:len(r.Data)+tc.padding]
								}
								var b bytes.Buffer
								codec.MustEncodeTo(&b, &r)
								return cbk(ctx, &b)
							}
							return errors.New(tc.err)
						})
			} else {
				f.mAtxS.EXPECT().
					Request(gomock.Any(), peer, epochIDBytes).
					DoAndReturn(
						func(context.Context, p2p.Peer, []byte, ...string) ([]byte, error) {
							if tc.err == "" {
								var data []byte
								expected, data = generateEpochData(t)
								return data, nil
							}
							return nil, errors.New(tc.err)
						})
			}
			got, err := f.PeerEpochInfo(context.Background(), peer, types.EpochID(111))
			if tc.err == "" {
				require.NoError(t, err)
				require.Equal(t, expected, got)
			} else {
				require.ErrorContains(t, err, tc.err)
			}
		})
	}
}

func TestFetch_GetMeshHashes(t *testing.T) {
	peer := p2p.Peer("p0")
	errUnknown := errors.New("unknown")
	tt := []struct {
		name     string
		params   [3]uint32 // from, to, by
		expected int
		err      error
	}{
		{
			name:     "success",
			params:   [3]uint32{7, 23, 5},
			expected: 5,
		},
		{
			name:   "failure",
			params: [3]uint32{7, 23, 5},
			err:    errUnknown,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := createFetch(t)
			req := &MeshHashRequest{
				From: types.LayerID(tc.params[0]),
				To:   types.LayerID(tc.params[1]),
				Step: tc.params[2],
			}
			var expected MeshHashes
			if tc.err == nil {
				hashes := make([]types.Hash32, tc.expected)
				for i := range hashes {
					hashes[i] = types.RandomHash()
				}
				expected.Hashes = hashes
			}
			reqData, err := codec.Encode(req)
			require.NoError(t, err)
			f.mMHashS.EXPECT().
				Request(gomock.Any(), peer, gomock.Any()).
				DoAndReturn(
					func(
						_ context.Context,
						_ p2p.Peer,
						gotReq []byte,
						extraProtocols ...string,
					) ([]byte, error) {
						require.Equal(t, reqData, gotReq)
						if tc.err == nil {
							data, err := codec.EncodeSlice(expected.Hashes)
							require.NoError(t, err)
							return data, nil
						}
						return nil, tc.err
					})
			got, err := f.PeerMeshHashes(context.Background(), peer, req)
			if tc.err == nil {
				require.NoError(t, err)
				require.Equal(t, expected, *got)
			} else {
				require.ErrorIs(t, err, tc.err)
			}
		})
	}
}

func TestFetch_GetCert(t *testing.T) {
	peers := []p2p.Peer{"p0", "p1", "p2"}
	errUnknown := errors.New("unknown")
	tt := []struct {
		name    string
		results [3]error

		err bool
	}{
		{
			name:    "success",
			results: [3]error{errUnknown, nil, nil},
		},
		{
			name:    "failure",
			results: [3]error{errUnknown, errUnknown, errUnknown},
			err:     true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := createFetch(t)
			lid := types.LayerID(11)
			bid := types.BlockID{1, 2, 3}
			req := &OpinionRequest{
				Layer: lid,
				Block: &bid,
			}
			expected := types.Certificate{BlockID: bid}
			reqData, err := codec.Encode(req)
			require.NoError(t, err)
			for i, peer := range peers {
				f.mOpn2S.EXPECT().
					Request(gomock.Any(), peer, gomock.Any()).
					DoAndReturn(func(
						_ context.Context,
						_ p2p.Peer,
						gotReq []byte,
						extraProtocols ...string,
					) ([]byte, error) {
						require.Equal(t, reqData, gotReq)
						if tc.results[i] == nil {
							data, err := codec.Encode(&expected)
							require.NoError(t, err)
							return data, nil
						}
						return nil, tc.results[i]
					})
				if tc.results[i] == nil {
					break
				}
			}
			got, err := f.GetCert(context.Background(), lid, bid, peers)
			if tc.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, expected, *got)
			}
		})
	}
}

// Test if GetAtxs() limits the number of concurrent requests to `cfg.GetAtxsConcurrency`.
func Test_GetAtxsLimiting(t *testing.T) {
	mesh, err := mocknet.FullMeshConnected(2)
	require.NoError(t, err)

	const (
		totalRequests     = 100
		getAtxConcurrency = 10
	)

	for _, withLimiting := range []bool{false, true} {
		t.Run(fmt.Sprintf("with limiting: %v", withLimiting), func(t *testing.T) {
			srv := server.New(
				wrapHost(mesh.Hosts()[1]),
				hashProtocol,
				server.WrapHandler(func(_ context.Context, data []byte) ([]byte, error) {
					var requestBatch RequestBatch
					require.NoError(t, codec.Decode(data, &requestBatch))
					resBatch := ResponseBatch{
						ID: requestBatch.ID,
					}
					if withLimiting {
						// should do only `cfg.GetAtxsConcurrency` requests at a time even though batch size is 1000
						require.Len(t, requestBatch.Requests, getAtxConcurrency)
					} else {
						require.Len(t, requestBatch.Requests, totalRequests)
					}
					for _, r := range requestBatch.Requests {
						resBatch.Responses = append(resBatch.Responses, ResponseMessage{Hash: r.Hash})
					}
					response, err := codec.Encode(&resBatch)
					require.NoError(t, err)
					return response, nil
				}),
			)

			var (
				eg          errgroup.Group
				ctx, cancel = context.WithCancel(context.Background())
			)
			defer cancel()
			eg.Go(func() error {
				return srv.Run(ctx)
			})
			t.Cleanup(func() {
				assert.NoError(t, eg.Wait())
			})

			cfg := DefaultConfig()
			// should do only `cfg.GetAtxsConcurrency` requests at a time even though batch size is 1000
			cfg.BatchSize = 1000
			cfg.QueueSize = 1000
			cfg.GetAtxsConcurrency = getAtxConcurrency

			cdb := datastore.NewCachedDB(statesql.InMemory(), zaptest.NewLogger(t))
			client := server.New(wrapHost(mesh.Hosts()[0]), hashProtocol, nil)
			host, err := p2p.Upgrade(mesh.Hosts()[0])
			require.NoError(t, err)
			f, err := NewFetch(cdb, store.New(), host,
				WithContext(context.Background()),
				withServers(map[string]requester{hashProtocol: client}),
				WithConfig(cfg),
			)
			require.NoError(t, err)

			atxValidatorMock := mocks.NewMockSyncValidator(gomock.NewController(t))
			f.validators = &dataValidators{
				atx: atxValidatorMock,
			}
			require.NoError(t, f.Start())
			t.Cleanup(f.Stop)

			var atxIds []types.ATXID
			for i := 0; i < totalRequests; i++ {
				id := types.RandomATXID()
				atxIds = append(atxIds, id)
				atxValidatorMock.EXPECT().HandleMessage(gomock.Any(), id.Hash32(), mesh.Hosts()[1].ID(), gomock.Any())
			}

			if withLimiting {
				err = f.GetAtxs(context.Background(), atxIds)
			} else {
				err = f.GetAtxs(context.Background(), atxIds, system.WithoutLimiting())
			}
			require.NoError(t, err)
		})
	}
}

func TestBatchErrorIgnore(t *testing.T) {
	for _, tc := range []struct {
		desc    string
		error   BatchError
		ignored bool
	}{
		{
			desc:    "empty",
			ignored: true,
		},
		{
			desc: "random error",
			error: BatchError{
				Errors: map[types.Hash32]error{
					{1}: errors.New("random error"),
				},
			},
		},
		{
			desc: "reject",
			error: BatchError{
				Errors: map[types.Hash32]error{
					{1}: pubsub.ErrValidationReject,
				},
			},
			ignored: true,
		},
		{
			desc: "ignore",
			error: BatchError{
				Errors: map[types.Hash32]error{
					{1}: ErrIgnore,
				},
			},
			ignored: true,
		},
		{
			desc: "recursive reject",
			error: BatchError{
				Errors: map[types.Hash32]error{
					{1}: &BatchError{
						Errors: map[types.Hash32]error{
							{2}: pubsub.ErrValidationReject,
						},
					},
				},
			},
			ignored: true,
		},
		{
			desc: "recursive ignore",
			error: BatchError{
				Errors: map[types.Hash32]error{
					{1}: &BatchError{
						Errors: map[types.Hash32]error{
							{2}: ErrIgnore,
						},
					},
				},
			},
			ignored: true,
		},
		{
			desc: "random error with reject",
			error: BatchError{
				Errors: map[types.Hash32]error{
					{1}: &BatchError{
						Errors: map[types.Hash32]error{
							{2}: pubsub.ErrValidationReject,
						},
					},
					{3}: errors.New("random error"),
				},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.ignored, tc.error.Ignore())
		})
	}
}

func FuzzCertRequest(f *testing.F) {
	h := createTestHandler(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		h.handleLayerOpinionsReq2(context.Background(), data)
	})
}

func FuzzMeshHashRequest(f *testing.F) {
	h := createTestHandler(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		h.handleMeshHashReq(context.Background(), data)
	})
}

func FuzzMeshHashRequestStream(f *testing.F) {
	h := createTestHandler(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		var b bytes.Buffer
		h.handleMeshHashReqStream(context.Background(), data, &b)
	})
}

func FuzzLayerInfo(f *testing.F) {
	h := createTestHandler(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		h.handleEpochInfoReq(context.Background(), data)
	})
}

func FuzzLayerInfoStream(f *testing.F) {
	h := createTestHandler(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		var b bytes.Buffer
		h.handleEpochInfoReqStream(context.Background(), data, &b)
	})
}

func FuzzHashReq(f *testing.F) {
	h := createTestHandler(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		h.handleHashReq(context.Background(), data)
	})
}

func FuzzHashReqStream(f *testing.F) {
	h := createTestHandler(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		var b bytes.Buffer
		h.handleHashReqStream(context.Background(), data, &b)
	})
}
