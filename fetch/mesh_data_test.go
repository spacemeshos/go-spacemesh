package fetch

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	txsForBlock    = iota
	txsForProposal = iota
)

const layersPerEpoch = 3

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)

	res := m.Run()
	os.Exit(res)
}

func (f *testFetch) withMethod(method int) *testFetch {
	f.method = method
	return f
}

func (f *testFetch) expectTransactionCall(times int) *gomock.Call {
	if f.method == txsForBlock {
		return f.mTxBlocksH.EXPECT().HandleMessage(gomock.Any(), gomock.Any(), gomock.Any()).Times(times)
	} else if f.method == txsForProposal {
		return f.mTxProposalH.EXPECT().HandleMessage(gomock.Any(), gomock.Any(), gomock.Any()).Times(times)
	}
	return nil
}

func (f *testFetch) testGetTxs(tids []types.TransactionID) error {
	if f.method == txsForBlock {
		return f.GetBlockTxs(context.TODO(), tids)
	} else if f.method == txsForProposal {
		return f.GetProposalTxs(context.TODO(), tids)
	}
	return nil
}

const (
	numBallots   = 10
	numBlocks    = 3
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
					require.NoError(t, req.validator(req.ctx, p2p.NoPeer, []byte{}))
					close(req.promise.completed)
					delete(f.unprocessed, h)
				}
				f.mu.Unlock()
			}
		}
	})
}

func generateMaliciousIDs(t *testing.T) []byte {
	t.Helper()
	var malicious MaliciousIDs
	for i := 0; i < numMalicious; i++ {
		malicious.NodeIDs = append(malicious.NodeIDs, types.RandomNodeID())
	}
	data, err := codec.Encode(&malicious)
	require.NoError(t, err)
	return data
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
	tt := []struct {
		name      string
		fetchErrs map[types.Hash32]struct{}
		hdlrErr   error
	}{
		{
			name: "all hashes fetched",
		},
		{
			name:      "all hashes failed",
			fetchErrs: map[types.Hash32]struct{}{hashes[0]: {}, hashes[1]: {}, hashes[2]: {}},
		},
		{
			name:      "some hashes failed",
			fetchErrs: map[types.Hash32]struct{}{hashes[1]: {}},
		},
		{
			name:    "handler failed",
			hdlrErr: errors.New("unknown"),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := createFetch(t)
			f.cfg.QueueSize = 3
			f.cfg.BatchSize = 2
			f.cfg.MaxRetriesForRequest = 0
			f.cfg.MaxRetriesForPeer = 0
			peers := []p2p.Peer{p2p.Peer("buddy 0"), p2p.Peer("buddy 1")}
			f.mh.EXPECT().GetPeers().Return(peers)
			f.mh.EXPECT().ID().Return(p2p.Peer("self")).AnyTimes()
			f.RegisterPeerHashes(peers[0], hashes[:2])
			f.RegisterPeerHashes(peers[1], hashes[2:])

			responses := make(map[types.Hash32]ResponseMessage)
			for _, h := range hashes {
				res := ResponseMessage{
					Hash: h,
					Data: []byte("a"),
				}
				responses[h] = res
			}
			f.mHashS.EXPECT().Request(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, p p2p.Peer, req []byte, okFunc func([]byte), _ func(error)) error {
					var rb RequestBatch
					err := codec.Decode(req, &rb)
					require.NoError(t, err)

					resBatch := ResponseBatch{
						ID: rb.ID,
					}
					for _, r := range rb.Requests {
						if _, ok := tc.fetchErrs[r.Hash]; ok {
							continue
						}
						res := responses[r.Hash]
						resBatch.Responses = append(resBatch.Responses, res)
						f.mBlocksH.EXPECT().HandleMessage(gomock.Any(), p, res.Data).Return(tc.hdlrErr)
					}
					bts, err := codec.Encode(&resBatch)
					require.NoError(t, err)
					okFunc(bts)
					return nil
				}).Times(len(peers))

			got := f.getHashes(context.TODO(), hashes, datastore.BlockDB, f.validators.block.HandleMessage)
			if len(tc.fetchErrs) > 0 || tc.hdlrErr != nil {
				require.NotEmpty(t, got)
			} else {
				require.Empty(t, got)
			}
		})
	}
}

func TestFetch_GetMalfeasanceProofs(t *testing.T) {
	nodeIDs := []types.NodeID{{1}, {2}, {3}}
	f := createFetch(t)
	f.mMalH.EXPECT().HandleMessage(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(len(nodeIDs))

	stop := make(chan struct{}, 1)
	var eg errgroup.Group
	startTestLoop(t, f.Fetch, &eg, stop)

	require.NoError(t, f.GetMalfeasanceProofs(context.TODO(), nodeIDs))
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
	f.mBlocksH.EXPECT().HandleMessage(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(len(blockIDs))

	stop := make(chan struct{}, 1)
	var eg errgroup.Group
	startTestLoop(t, f.Fetch, &eg, stop)

	require.NoError(t, f.GetBlocks(context.TODO(), blockIDs))
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
	f.mBallotH.EXPECT().HandleMessage(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(len(ballotIDs))

	stop := make(chan struct{}, 1)
	var eg errgroup.Group
	startTestLoop(t, f.Fetch, &eg, stop)

	require.NoError(t, f.GetBallots(context.TODO(), ballotIDs))
	close(stop)
	require.NoError(t, eg.Wait())
}

func genLayerProposal(tb testing.TB, layerID types.LayerID, txs []types.TransactionID) *types.Proposal {
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
	p.Signature = signer.Sign(signing.BALLOT, p.SignedBytes())
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
	f.mProposalH.EXPECT().HandleMessage(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(len(proposalIDs))

	stop := make(chan struct{}, 1)
	var eg errgroup.Group
	startTestLoop(t, f.Fetch, &eg, stop)

	require.NoError(t, f.GetProposals(context.TODO(), proposalIDs))
	close(stop)
	require.NoError(t, eg.Wait())
}

func genTx(tb testing.TB, signer *signing.EdSigner, dest types.Address, amount, nonce, price uint64) types.Transaction {
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
		atx := types.NewActivationTx(types.NIPostChallenge{}, types.Address{1, 2, 3}, &types.NIPost{}, i, nil, nil)
		require.NoError(tb, activation.SignAndFinalizeAtx(sig, atx))
		atxs = append(atxs, atx)
	}
	return atxs
}

func TestGetATXs(t *testing.T) {
	atxs := genATXs(t, 2)
	f := createFetch(t)
	f.mAtxH.EXPECT().HandleMessage(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(len(atxs))

	stop := make(chan struct{}, 1)
	var eg errgroup.Group
	startTestLoop(t, f.Fetch, &eg, stop)

	atxIDs := types.ToATXIDs(atxs)
	require.NoError(t, f.GetAtxs(context.Background(), atxIDs))
	close(stop)
	require.NoError(t, eg.Wait())
}

func TestGetPoetProof(t *testing.T) {
	f := createFetch(t)
	h := types.RandomHash()
	f.mPoetH.EXPECT().HandleMessage(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

	stop := make(chan struct{}, 1)
	var eg errgroup.Group
	startTestLoop(t, f.Fetch, &eg, stop)

	require.NoError(t, f.GetPoetProof(context.TODO(), h))
	close(stop)
	require.NoError(t, eg.Wait())
}

func TestFetch_GetMaliciousIDs(t *testing.T) {
	peers := []p2p.Peer{"p0", "p1", "p3", "p4"}
	errUnknown := errors.New("unknown")
	tt := []struct {
		name string
		errs []error
	}{
		{
			name: "all peers returns",
			errs: []error{nil, nil, nil, nil},
		},
		{
			name: "some peers errors",
			errs: []error{nil, errUnknown, nil, errUnknown},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, len(peers), len(tc.errs))
			f := createFetch(t)
			oks := make(chan struct{}, len(peers))
			errs := make(chan struct{}, len(peers))
			var wg sync.WaitGroup
			wg.Add(len(peers))
			okFunc := func([]byte, p2p.Peer) {
				oks <- struct{}{}
				wg.Done()
			}
			errFunc := func(error, p2p.Peer) {
				errs <- struct{}{}
				wg.Done()
			}
			var expOk, expErr int
			for i, p := range peers {
				if tc.errs[i] == nil {
					expOk++
				} else {
					expErr++
				}
				idx := i
				f.mMalS.EXPECT().Request(gomock.Any(), p, []byte{}, gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, _ p2p.Peer, _ []byte, okCB func([]byte), errCB func(error)) error {
						if tc.errs[idx] == nil {
							go okCB(generateMaliciousIDs(t))
						} else {
							go errCB(tc.errs[idx])
						}
						return nil
					})
			}
			require.NoError(t, f.GetMaliciousIDs(context.TODO(), peers, okFunc, errFunc))
			wg.Wait()
			require.Len(t, oks, expOk)
			require.Len(t, errs, expErr)
		})
	}
}

func TestFetch_GetLayerData(t *testing.T) {
	peers := []p2p.Peer{"p0", "p1", "p3", "p4"}
	errUnknown := errors.New("unknown")
	tt := []struct {
		name string
		errs []error
	}{
		{
			name: "all peers returns",
			errs: []error{nil, nil, nil, nil},
		},
		{
			name: "some peers errors",
			errs: []error{nil, errUnknown, nil, errUnknown},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, len(peers), len(tc.errs))
			f := createFetch(t)
			oks := make(chan struct{}, len(peers))
			errs := make(chan struct{}, len(peers))
			var wg sync.WaitGroup
			wg.Add(len(peers))
			okFunc := func(data []byte, peer p2p.Peer) {
				oks <- struct{}{}
				wg.Done()
			}
			errFunc := func(err error, peer p2p.Peer) {
				errs <- struct{}{}
				wg.Done()
			}
			var expOk, expErr int
			for i, p := range peers {
				if tc.errs[i] == nil {
					expOk++
				} else {
					expErr++
				}
				idx := i
				f.mLyrS.EXPECT().Request(gomock.Any(), p, gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, _ p2p.Peer, _ []byte, okCB func([]byte), errCB func(error)) error {
						if tc.errs[idx] == nil {
							go okCB(generateLayerContent(t))
						} else {
							go errCB(tc.errs[idx])
						}
						return nil
					})
			}
			require.NoError(t, f.GetLayerData(context.TODO(), peers, types.LayerID(111), okFunc, errFunc))
			wg.Wait()
			require.Len(t, oks, expOk)
			require.Len(t, errs, expErr)
		})
	}
}

func TestFetch_GetLayerOpinions(t *testing.T) {
	peers := []p2p.Peer{"p0", "p1", "p3", "p4"}
	errUnknown := errors.New("unknown")
	tt := []struct {
		name string
		errs []error
	}{
		{
			name: "all peers returns",
			errs: []error{nil, nil, nil, nil},
		},
		{
			name: "some peers errors",
			errs: []error{nil, errUnknown, nil, errUnknown},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, len(peers), len(tc.errs))
			f := createFetch(t)
			oks := make(chan struct{}, len(peers))
			errs := make(chan struct{}, len(peers))
			var wg sync.WaitGroup
			wg.Add(len(peers))
			okFunc := func(data []byte, peer p2p.Peer) {
				oks <- struct{}{}
				wg.Done()
			}
			errFunc := func(err error, peer p2p.Peer) {
				errs <- struct{}{}
				wg.Done()
			}
			var expOk, expErr int
			for i, p := range peers {
				if tc.errs[i] == nil {
					expOk++
				} else {
					expErr++
				}
				idx := i
				f.mOpnS.EXPECT().Request(gomock.Any(), p, gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, _ p2p.Peer, _ []byte, okCB func([]byte), errCB func(error)) error {
						if tc.errs[idx] == nil {
							go okCB([]byte("data"))
						} else {
							go errCB(tc.errs[idx])
						}
						return nil
					})
			}
			require.NoError(t, f.GetLayerOpinions(context.TODO(), peers, types.LayerID(111), okFunc, errFunc))
			wg.Wait()
			require.Len(t, oks, expOk)
			require.Len(t, errs, expErr)
		})
	}
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
	errUnknown := errors.New("unknown")
	tt := []struct {
		name string
		err  error
	}{
		{
			name: "success",
		},
		{
			name: "fail",
			err:  errUnknown,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := createFetch(t)
			f.mh.EXPECT().ID().Return(p2p.Peer("self")).AnyTimes()
			var expected *EpochData
			f.mAtxS.EXPECT().Request(gomock.Any(), peer, gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ p2p.Peer, req []byte, okCB func([]byte), errCB func(error)) error {
					if tc.err == nil {
						var data []byte
						expected, data = generateEpochData(t)
						okCB(data)
					} else {
						errCB(tc.err)
					}
					return nil
				})
			got, err := f.PeerEpochInfo(context.TODO(), peer, types.EpochID(111))
			require.ErrorIs(t, err, tc.err)
			if tc.err == nil {
				require.Equal(t, expected, got)
			}
		})
	}
}

func TestFetch_GetMeshHashes(t *testing.T) {
	peer := p2p.Peer("p0")
	errUnknown := errors.New("unknown")
	tt := []struct {
		name     string
		params   [4]uint32 // from, to, delta, steps
		expected []types.LayerID
		err      error
	}{
		{
			name:   "success",
			params: [4]uint32{7, 23, 5, 4},
			expected: []types.LayerID{
				types.LayerID(7),
				types.LayerID(12),
				types.LayerID(17),
				types.LayerID(22),
				types.LayerID(23),
			},
		},
		{
			name:   "failure",
			params: [4]uint32{7, 23, 5, 4},
			err:    errUnknown,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := createFetch(t)
			req := &MeshHashRequest{
				From:  types.LayerID(tc.params[0]),
				To:    types.LayerID(tc.params[1]),
				Delta: tc.params[2],
				Steps: tc.params[3],
			}
			var expected MeshHashes
			if tc.err == nil {
				hashes := make([]types.Hash32, len(tc.expected))
				for i := range hashes {
					hashes[i] = types.RandomHash()
				}
				expected.Layers = tc.expected
				expected.Hashes = hashes
			}
			reqData, err := codec.Encode(req)
			require.NoError(t, err)
			f.mMHashS.EXPECT().Request(gomock.Any(), peer, gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ p2p.Peer, gotReq []byte, okCB func([]byte), errCB func(error)) error {
					require.Equal(t, reqData, gotReq)
					if tc.err == nil {
						data, err := codec.EncodeSlice(expected.Hashes)
						require.NoError(t, err)
						okCB(data)
					} else {
						errCB(tc.err)
					}
					return nil
				})
			got, err := f.PeerMeshHashes(context.TODO(), peer, req)
			if tc.err == nil {
				require.NoError(t, err)
				require.Equal(t, expected, *got)
			} else {
				require.ErrorIs(t, err, tc.err)
			}
		})
	}
}

func FuzzMeshHashRequest(f *testing.F) {
	h := createTestHandler(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		h.handleMeshHashReq(context.TODO(), data)
	})
}

func FuzzLayerInfo(f *testing.F) {
	h := createTestHandler(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		h.handleEpochInfoReq(context.TODO(), data)
	})
}

func FuzzHashReq(f *testing.F) {
	h := createTestHandler(f)
	f.Fuzz(func(t *testing.T, data []byte) {
		h.handleHashReq(context.TODO(), data)
	})
}
