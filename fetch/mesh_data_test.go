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
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
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

func (f *testFetch) expectTransactionCall(data []byte) *gomock.Call {
	if f.method == txsForBlock {
		return f.mTxH.EXPECT().HandleBlockTransaction(gomock.Any(), data)
	} else if f.method == txsForProposal {
		return f.mTxH.EXPECT().HandleProposalTransaction(gomock.Any(), data)
	}
	return nil
}

func (f *testFetch) getTxs(tids []types.TransactionID) error {
	if f.method == txsForBlock {
		return f.GetBlockTxs(context.TODO(), tids)
	} else if f.method == txsForProposal {
		return f.GetProposalTxs(context.TODO(), tids)
	}
	return nil
}

const (
	numBallots = 10
	numBlocks  = 3
)

func startTestLoop(f *Fetch, eg *errgroup.Group, hdlr func(*request)) {
	eg.Go(func() error {
		f.loop(hdlr)
		return nil
	})
}

func generateLayerContent(t *testing.T) []byte {
	t.Helper()
	ballotIDs := make([]types.BallotID, 0, numBallots)
	for i := 0; i < numBallots; i++ {
		ballotIDs = append(ballotIDs, types.RandomBallotID())
	}
	blockIDs := make([]types.BlockID, 0, numBlocks)
	for i := 0; i < numBlocks; i++ {
		blockIDs = append(blockIDs, types.RandomBlockID())
	}
	lb := LayerData{
		Ballots: ballotIDs,
		Blocks:  blockIDs,
	}
	out, _ := codec.Encode(&lb)
	return out
}

func TestGetBlocks(t *testing.T) {
	blks := []*types.Block{
		types.GenLayerBlock(types.NewLayerID(10), types.RandomTXSet(10)),
		types.GenLayerBlock(types.NewLayerID(20), types.RandomTXSet(10)),
	}
	blockIDs := types.ToBlockIDs(blks)
	hashes := types.BlockIDsToHashes(blockIDs)
	errUnknown := errors.New("unknown")
	tt := []struct {
		name         string
		fetchErrs    []error
		hdlrErr, err error
	}{
		{
			name:      "all hashes fetched",
			fetchErrs: []error{nil, nil},
		},
		{
			name:      "all hashes failed",
			fetchErrs: []error{errUnknown, errUnknown},
			err:       errUnknown,
		},
		{
			name:      "some hashes failed",
			fetchErrs: []error{nil, errUnknown},
			err:       errUnknown,
		},
		{
			name:      "handler failed",
			fetchErrs: []error{nil, nil},
			hdlrErr:   errUnknown,
			err:       errUnknown,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Len(t, tc.fetchErrs, len(hashes))
			f := createFetch(t)
			results := make(map[types.Hash32]HashDataPromiseResult, len(hashes))
			for i, h := range hashes {
				if tc.fetchErrs[i] == nil {
					data, err := codec.Encode(blks[i])
					require.NoError(t, err)
					results[h] = HashDataPromiseResult{
						Hash: h,
						Data: data,
					}
					f.mBlocksH.EXPECT().HandleSyncedBlock(gomock.Any(), data).Return(tc.hdlrErr)
				} else {
					results[h] = HashDataPromiseResult{
						Hash: h,
						Err:  tc.fetchErrs[i],
					}
				}
			}

			var eg errgroup.Group
			startTestLoop(f.Fetch, &eg, func(req *request) {
				req.returnChan <- results[req.hash]
			})

			require.ErrorIs(t, f.GetBlocks(context.TODO(), blockIDs), tc.err)
			f.cancel()
			eg.Wait()
		})
	}
}

func TestGetBallots(t *testing.T) {
	blts := []*types.Ballot{
		types.GenLayerBallot(types.NewLayerID(10)),
		types.GenLayerBallot(types.NewLayerID(20)),
	}
	ballotIDs := types.ToBallotIDs(blts)
	hashes := types.BallotIDsToHashes(ballotIDs)
	errUnknown := errors.New("unknown")
	tt := []struct {
		name         string
		fetchErrs    []error
		hdlrErr, err error
	}{
		{
			name:      "all hashes fetched",
			fetchErrs: []error{nil, nil},
		},
		{
			name:      "all hashes failed",
			fetchErrs: []error{errUnknown, errUnknown},
			err:       errUnknown,
		},
		{
			name:      "some hashes failed",
			fetchErrs: []error{nil, errUnknown},
			err:       errUnknown,
		},
		{
			name:      "handler failed",
			fetchErrs: []error{nil, nil},
			hdlrErr:   errUnknown,
			err:       errUnknown,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Len(t, tc.fetchErrs, len(hashes))
			f := createFetch(t)
			results := make(map[types.Hash32]HashDataPromiseResult, len(hashes))
			for i, h := range hashes {
				if tc.fetchErrs[i] == nil {
					data, err := codec.Encode(blts[i])
					require.NoError(t, err)
					results[h] = HashDataPromiseResult{
						Hash: h,
						Data: data,
					}
					f.mBallotH.EXPECT().HandleSyncedBallot(gomock.Any(), data).Return(tc.hdlrErr)
				} else {
					results[h] = HashDataPromiseResult{
						Hash: h,
						Err:  tc.fetchErrs[i],
					}
				}
			}

			var eg errgroup.Group
			startTestLoop(f.Fetch, &eg, func(req *request) {
				req.returnChan <- results[req.hash]
			})

			require.ErrorIs(t, f.GetBallots(context.TODO(), ballotIDs), tc.err)
			f.cancel()
			eg.Wait()
		})
	}
}

func TestGetProposals(t *testing.T) {
	proposals := []*types.Proposal{
		types.GenLayerProposal(types.NewLayerID(10), nil),
		types.GenLayerProposal(types.NewLayerID(20), nil),
	}
	proposalIDs := types.ToProposalIDs(proposals)
	hashes := types.ProposalIDsToHashes(proposalIDs)
	errUnknown := errors.New("unknown")
	tt := []struct {
		name         string
		fetchErrs    []error
		hdlrErr, err error
	}{
		{
			name:      "all hashes fetched",
			fetchErrs: []error{nil, nil},
		},
		{
			name:      "all hashes failed",
			fetchErrs: []error{errUnknown, errUnknown},
			err:       errUnknown,
		},
		{
			name:      "some hashes failed",
			fetchErrs: []error{nil, errUnknown},
			err:       errUnknown,
		},
		{
			name:      "handler failed",
			fetchErrs: []error{nil, nil},
			hdlrErr:   errUnknown,
			err:       errUnknown,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Len(t, tc.fetchErrs, len(hashes))
			f := createFetch(t)
			results := make(map[types.Hash32]HashDataPromiseResult, len(hashes))
			for i, h := range hashes {
				if tc.fetchErrs[i] == nil {
					data, err := codec.Encode(proposals[i])
					require.NoError(t, err)
					results[h] = HashDataPromiseResult{
						Hash: h,
						Data: data,
					}
					f.mProposalH.EXPECT().HandleSyncedProposal(gomock.Any(), data).Return(tc.hdlrErr)
				} else {
					results[h] = HashDataPromiseResult{
						Hash: h,
						Err:  tc.fetchErrs[i],
					}
				}
			}

			var eg errgroup.Group
			startTestLoop(f.Fetch, &eg, func(req *request) {
				req.returnChan <- results[req.hash]
			})

			require.ErrorIs(t, f.GetProposals(context.TODO(), proposalIDs), tc.err)
			f.cancel()
			eg.Wait()
		})
	}
}

func genTx(t *testing.T, signer *signing.EdSigner, dest types.Address, amount, nonce, price uint64) types.Transaction {
	t.Helper()
	raw := wallet.Spend(signer.PrivateKey(), dest, amount,
		types.Nonce{Counter: nonce},
	)
	tx := types.Transaction{
		RawTx:    types.NewRawTx(raw),
		TxHeader: &types.TxHeader{},
	}
	tx.MaxGas = 100
	tx.MaxSpend = amount
	tx.GasPrice = price
	tx.Nonce = types.Nonce{Counter: nonce}
	tx.Principal = types.GenerateAddress(signer.PublicKey().Bytes())
	return tx
}

func genTransactions(t *testing.T, num int) []*types.Transaction {
	t.Helper()
	txs := make([]*types.Transaction, 0, num)
	for i := 0; i < num; i++ {
		tx := genTx(t, signing.NewEdSigner(), types.Address{1}, 1, 1, 1)
		txs = append(txs, &tx)
	}
	return txs
}

func TestGetTxs_FetchSomeError(t *testing.T) {
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
			hashes := types.TransactionIDsToHashes(tids)

			errUnknown := errors.New("unknown")
			results := make(map[types.Hash32]HashDataPromiseResult, len(hashes))
			for i, h := range hashes {
				if i == 0 {
					results[h] = HashDataPromiseResult{
						Hash: h,
						Err:  errUnknown,
					}
				} else {
					data, err := codec.Encode(&tids[i])
					require.NoError(t, err)
					results[h] = HashDataPromiseResult{
						Hash: h,
						Data: data,
					}
					f.expectTransactionCall(data).Return(nil).Times(1)
				}
			}

			var eg errgroup.Group
			startTestLoop(f.Fetch, &eg, func(req *request) {
				req.returnChan <- results[req.hash]
			})

			require.ErrorIs(t, f.getTxs(tids), errUnknown)
			f.cancel()
			eg.Wait()
		})
	}
}

func TestGetTxs(t *testing.T) {
	errUnknown := errors.New("unknown")
	txs := genTransactions(t, 19)
	tids := types.ToTransactionIDs(txs)
	hashes := types.TransactionIDsToHashes(tids)
	tt := []struct {
		name         string
		hdlrErr, err error
	}{
		{
			name: "all hashes fetched",
		},
		{
			name:    "handler error",
			hdlrErr: errUnknown,
			err:     errUnknown,
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := createFetch(t)
			results := make(map[types.Hash32]HashDataPromiseResult, len(hashes))
			for i, h := range hashes {
				data, err := codec.Encode(&tids[i])
				require.NoError(t, err)
				results[h] = HashDataPromiseResult{
					Hash: h,
					Data: data,
				}
				f.mTxH.EXPECT().HandleBlockTransaction(gomock.Any(), data).Return(tc.hdlrErr).Times(1)
			}

			var eg errgroup.Group
			startTestLoop(f.Fetch, &eg, func(req *request) {
				req.returnChan <- results[req.hash]
			})

			require.ErrorIs(t, f.GetBlockTxs(context.TODO(), tids), tc.err)
			f.cancel()
			eg.Wait()
		})
	}
}

func genATXs(t *testing.T, num int) []*types.ActivationTx {
	t.Helper()
	sig := signing.NewEdSigner()
	atxs := make([]*types.ActivationTx, 0, num)
	for i := 0; i < num; i++ {
		atx := types.NewActivationTx(types.NIPostChallenge{}, types.Address{1, 2, 3}, &types.NIPost{}, uint(i), nil)
		require.NoError(t, activation.SignAtx(sig, atx))
		require.NoError(t, atx.CalcAndSetID())
		require.NoError(t, atx.CalcAndSetNodeID())
		atxs = append(atxs, atx)
	}
	return atxs
}

func TestGetATXs(t *testing.T) {
	atxs := genATXs(t, 2)
	atxIDs := types.ToATXIDs(atxs)
	hashes := types.ATXIDsToHashes(atxIDs)
	errUnknown := errors.New("unknown")
	tt := []struct {
		name         string
		fetchErrs    []error
		hdlrErr, err error
	}{
		{
			name:      "all hashes fetched",
			fetchErrs: []error{nil, nil},
		},
		{
			name:      "all hashes failed",
			fetchErrs: []error{errUnknown, errUnknown},
			err:       errUnknown,
		},
		{
			name:      "some hashes failed",
			fetchErrs: []error{nil, errUnknown},
			err:       errUnknown,
		},
		{
			name:      "handler failed",
			fetchErrs: []error{nil, nil},
			hdlrErr:   errUnknown,
			err:       errUnknown,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Len(t, tc.fetchErrs, len(hashes))
			f := createFetch(t)
			results := make(map[types.Hash32]HashDataPromiseResult, len(hashes))
			for i, h := range hashes {
				if tc.fetchErrs[i] == nil {
					data, err := codec.Encode(atxs[i])
					require.NoError(t, err)
					results[h] = HashDataPromiseResult{
						Hash: h,
						Data: data,
					}
					f.mAtxH.EXPECT().HandleAtxData(gomock.Any(), data).Return(tc.hdlrErr)
				} else {
					results[h] = HashDataPromiseResult{
						Hash: h,
						Err:  tc.fetchErrs[i],
					}
				}
			}

			var eg errgroup.Group
			startTestLoop(f.Fetch, &eg, func(req *request) {
				req.returnChan <- results[req.hash]
			})

			require.ErrorIs(t, f.GetAtxs(context.TODO(), atxIDs), tc.err)
			f.cancel()
			eg.Wait()
		})
	}
}

func TestGetPoetProof(t *testing.T) {
	f := createFetch(t)
	proof := types.PoetProofMessage{}
	h := types.RandomHash()
	data, err := codec.Encode(&proof)
	require.NoError(t, err)

	var eg errgroup.Group
	startTestLoop(f.Fetch, &eg, func(req *request) {
		req.returnChan <- HashDataPromiseResult{
			Hash: h,
			Data: data,
		}
	})

	f.mPoetH.EXPECT().ValidateAndStoreMsg(data).Return(nil).Times(1)
	require.NoError(t, f.GetPoetProof(context.TODO(), h))

	f.mPoetH.EXPECT().ValidateAndStoreMsg(data).Return(sql.ErrObjectExists).Times(1)
	require.NoError(t, f.GetPoetProof(context.TODO(), h))

	f.mPoetH.EXPECT().ValidateAndStoreMsg(data).Return(errors.New("unknown")).Times(1)
	require.Error(t, f.GetPoetProof(context.TODO(), h))
	f.cancel()
	eg.Wait()
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
					func(_ context.Context, _ p2p.Peer, _ []byte, okFunc func([]byte), errFunc func(error)) error {
						if tc.errs[idx] == nil {
							go okFunc(generateLayerContent(t))
						} else {
							go errFunc(tc.errs[idx])
						}
						return nil
					})
			}
			require.NoError(t, f.GetLayerData(context.TODO(), peers, types.NewLayerID(111), okFunc, errFunc))
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
					func(_ context.Context, _ p2p.Peer, _ []byte, okFunc func([]byte), errFunc func(error)) error {
						if tc.errs[idx] == nil {
							go okFunc([]byte("data"))
						} else {
							go errFunc(tc.errs[idx])
						}
						return nil
					})
			}
			require.NoError(t, f.GetLayerOpinions(context.TODO(), peers, types.NewLayerID(111), okFunc, errFunc))
			wg.Wait()
			require.Len(t, oks, expOk)
			require.Len(t, errs, expErr)
		})
	}
}

func TestFetch_GetEpochATXIDs(t *testing.T) {
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
			var wg sync.WaitGroup
			wg.Add(1)
			okFunc := func(_ []byte) {
				wg.Done()
			}
			errFunc := func(err error) {
				require.ErrorIs(t, err, tc.err)
				wg.Done()
			}
			f.mAtxS.EXPECT().Request(gomock.Any(), peer, gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ p2p.Peer, req []byte, okFunc func([]byte), _ func(error)) error {
					if tc.err == nil {
						go okFunc([]byte("a"))
					} else {
						go errFunc(tc.err)
					}
					return nil
				})
			require.NoError(t, f.GetEpochATXIDs(context.TODO(), peer, types.EpochID(111), okFunc, errFunc))
			wg.Wait()
		})
	}
}
