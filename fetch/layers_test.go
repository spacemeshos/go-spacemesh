package fetch

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/fetch/mocks"
	ftypes "github.com/spacemeshos/go-spacemesh/fetch/types"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

const (
	txsForBlock    = iota
	txsForProposal = iota
)

type testLogic struct {
	*Logic
	mMesh      *mocks.MockmeshProvider
	mAtxH      *mocks.MockatxHandler
	mBallotH   *mocks.MockballotHandler
	mBlocksH   *mocks.MockblockHandler
	mCertH     *mocks.MockcertHandler
	mProposalH *mocks.MockproposalHandler
	method     int
	mTxH       *mocks.MocktxHandler
	mPoetH     *mocks.MockpoetHandler
	mFetcher   *mocks.Mockfetcher
}

func (l *testLogic) withMethod(method int) *testLogic {
	l.method = method
	return l
}

func (l *testLogic) expectTransactionCall(data []byte) *gomock.Call {
	if l.method == txsForBlock {
		return l.mTxH.EXPECT().HandleBlockTransaction(gomock.Any(), data)
	} else if l.method == txsForProposal {
		return l.mTxH.EXPECT().HandleProposalTransaction(gomock.Any(), data)
	}
	return nil
}

func (l *testLogic) getTxs(tids []types.TransactionID) error {
	if l.method == txsForBlock {
		return l.GetBlockTxs(context.TODO(), tids)
	} else if l.method == txsForProposal {
		return l.GetProposalTxs(context.TODO(), tids)
	}
	return nil
}

func createTestLogic(t *testing.T) *testLogic {
	ctrl := gomock.NewController(t)
	tl := &testLogic{
		mMesh:      mocks.NewMockmeshProvider(ctrl),
		mAtxH:      mocks.NewMockatxHandler(ctrl),
		mBallotH:   mocks.NewMockballotHandler(ctrl),
		mBlocksH:   mocks.NewMockblockHandler(ctrl),
		mCertH:     mocks.NewMockcertHandler(ctrl),
		mProposalH: mocks.NewMockproposalHandler(ctrl),
		mTxH:       mocks.NewMocktxHandler(ctrl),
		mPoetH:     mocks.NewMockpoetHandler(ctrl),
		mFetcher:   mocks.NewMockfetcher(ctrl),
	}
	tl.Logic = &Logic{
		log:             logtest.New(t),
		db:              sql.InMemory(),
		layerBlocksRes:  make(map[types.LayerID]*layerResult),
		layerBlocksChs:  make(map[types.LayerID][]chan LayerPromiseResult),
		msh:             tl.mMesh,
		atxHandler:      tl.mAtxH,
		ballotHandler:   tl.mBallotH,
		blockHandler:    tl.mBlocksH,
		certHandler:     tl.mCertH,
		proposalHandler: tl.mProposalH,
		txHandler:       tl.mTxH,
		poetHandler:     tl.mPoetH,
		fetcher:         tl.mFetcher,
	}
	return tl
}

const (
	numBallots = 10
	numBlocks  = 3
)

func generateLayerContent(t *testing.T, processed types.LayerID, hareOutput ...*types.BlockID) []byte {
	require.LessOrEqual(t, len(hareOutput), 1)
	ballotIDs := make([]types.BallotID, 0, numBallots)
	for i := 0; i < numBallots; i++ {
		ballotIDs = append(ballotIDs, types.RandomBallotID())
	}
	blockIDs := make([]types.BlockID, 0, numBlocks)
	for i := 0; i < numBlocks; i++ {
		blockIDs = append(blockIDs, types.RandomBlockID())
	}
	hash := types.CalcBlocksHash32(types.SortBlockIDs(blockIDs), nil)
	certified := types.EmptyBlockID
	if len(hareOutput) == 1 {
		certified = *hareOutput[0]
	} else {
		certified = blockIDs[0]
	}
	lb := layerData{
		Ballots: ballotIDs,
		Blocks:  blockIDs,
		HareOutput: &types.Certificate{
			BlockID: certified,
		},
		ProcessedLayer: processed,
		Hash:           hash,
		AggregatedHash: types.RandomHash(),
	}
	out, _ := codec.Encode(lb)
	return out
}

func generateEmptyLayer() []byte {
	lb := layerData{
		Ballots: []types.BallotID{},
		Blocks:  []types.BlockID{},
		HareOutput: &types.Certificate{
			BlockID: types.EmptyBlockID,
		},
		ProcessedLayer: types.NewLayerID(10),
		Hash:           types.EmptyLayerHash,
		AggregatedHash: types.RandomHash(),
	}
	out, _ := codec.Encode(lb)
	return out
}

func genPeers(num int) []p2p.Peer {
	peers := make([]p2p.Peer, 0, num)
	for i := 0; i < num; i++ {
		peers = append(peers, p2p.Peer(fmt.Sprintf("peer_%d", i)))
	}
	return peers
}

func TestPollLayerContent(t *testing.T) {
	processed := types.NewLayerID(11)
	errUnknown := errors.New("unknown")
	tt := []struct {
		name                   string
		zeroBlock              bool
		ballotFail, blocksFail bool
		hareOutput             *types.BlockID
		certErr, err           error
	}{
		{
			name:       "all peers have layer data",
			hareOutput: &types.EmptyBlockID,
		},
		{
			name:       "empty hare output",
			hareOutput: &types.BlockID{},
		},
		{
			name:      "all peers have zero blocks",
			zeroBlock: true,
		},
		{
			name:       "ballots failure ignored",
			ballotFail: true,
		},
		{
			name:       "blocks failure ignored",
			blocksFail: true,
		},
		{
			name:       "cert non-existent block id",
			err:        errCertifiedBlockNotReferenced,
			hareOutput: &types.BlockID{1, 2, 3},
		},
		{
			name:    "cert error",
			certErr: errUnknown,
			err:     errInvalidCertificate,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			numPeers := 4
			peers := genPeers(numPeers)
			layerID := types.NewLayerID(10)
			tl := createTestLogic(t)
			tl.mFetcher.EXPECT().GetLayerData(gomock.Any(), layerID, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ types.LayerID, okCB func([]byte, p2p.Peer, int), errCB func(error, p2p.Peer, int)) error {
					for _, peer := range peers {
						if tc.zeroBlock {
							okCB(generateEmptyLayer(), peer, numPeers)
						} else {
							tl.mFetcher.EXPECT().RegisterPeerHashes(peer, gomock.Any())
							if tc.hareOutput != nil {
								okCB(generateLayerContent(t, processed, tc.hareOutput), peer, numPeers)
							} else {
								okCB(generateLayerContent(t, processed), peer, numPeers)
							}
						}
					}
					return nil
				})
			tl.mCertH.EXPECT().HandleSyncedCertificate(gomock.Any(), layerID, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ types.LayerID, got *types.Certificate) error {
					require.NotNil(t, got)
					if tc.zeroBlock {
						require.Equal(t, types.EmptyBlockID, got.BlockID)
					} else if tc.hareOutput != nil {
						require.Equal(t, *tc.hareOutput, got.BlockID)
					} else {
						require.NotEqual(t, types.EmptyBlockID, got.BlockID)
					}
					return tc.certErr
				}).Times(numPeers)
			if tc.ballotFail {
				tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BallotDB, false).DoAndReturn(
					func([]types.Hash32, datastore.Hint, bool) map[types.Hash32]chan ftypes.HashDataPromiseResult {
						ch := make(chan ftypes.HashDataPromiseResult, 1)
						ch <- ftypes.HashDataPromiseResult{
							Err: errInternal,
						}
						return map[types.Hash32]chan ftypes.HashDataPromiseResult{types.RandomHash(): ch}
					}).Times(numPeers)
				tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BlockDB, false).Return(nil).Times(numPeers)
			} else if tc.blocksFail {
				tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BallotDB, false).Return(nil).Times(numPeers)
				tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BlockDB, false).DoAndReturn(
					func([]types.Hash32, datastore.Hint, bool) map[types.Hash32]chan ftypes.HashDataPromiseResult {
						ch := make(chan ftypes.HashDataPromiseResult, 1)
						ch <- ftypes.HashDataPromiseResult{
							Err: errInternal,
						}
						return map[types.Hash32]chan ftypes.HashDataPromiseResult{types.RandomHash(): ch}
					}).Times(numPeers)
			} else if !tc.zeroBlock {
				tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BallotDB, false).Return(nil).Times(numPeers)
				tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BlockDB, false).Return(nil).Times(numPeers)
			}
			if tc.zeroBlock {
				tl.mMesh.EXPECT().SetZeroBlockLayer(gomock.Any(), layerID)
			}

			res := <-tl.PollLayerContent(context.TODO(), layerID)
			if tc.err != nil {
				require.ErrorIs(t, res.Err, tc.err)
			} else {
				assert.NoError(t, res.Err)
				assert.Equal(t, layerID, res.Layer)
				got, err := layers.GetHareOutput(tl.db, layerID)
				require.NoError(t, err)
				if tc.zeroBlock {
					require.Equal(t, types.EmptyBlockID, got)
				} else if tc.hareOutput != nil {
					require.Equal(t, *tc.hareOutput, got)
				} else {
					require.NotEqual(t, types.EmptyBlockID, got)
				}
			}
		})
	}
}

func TestPollLayerContent_PeerErrors(t *testing.T) {
	processed := types.NewLayerID(11)
	numPeers := 4
	peers := genPeers(numPeers)
	err := errors.New("not available")

	tt := []struct {
		name                       string
		errs                       []error
		responses                  [][]byte
		emptyHareOutput, zeroBlock bool
	}{
		{
			name: "only one peer has data",
			errs: []error{err, nil, err, err},
		},
		{
			name:            "only one peer has empty layer",
			errs:            []error{err, nil, err, err},
			emptyHareOutput: true,
			zeroBlock:       true,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Len(t, tc.errs, numPeers)
			layerID := types.NewLayerID(10)
			tl := createTestLogic(t)
			tl.mFetcher.EXPECT().GetLayerData(gomock.Any(), layerID, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ types.LayerID, okCB func([]byte, p2p.Peer, int), errCB func(error, p2p.Peer, int)) error {
					for i, peer := range peers {
						if tc.errs[i] == nil {
							if tc.zeroBlock {
								okCB(generateEmptyLayer(), peer, numPeers)
							} else {
								tl.mFetcher.EXPECT().RegisterPeerHashes(peer, gomock.Any())
								if tc.emptyHareOutput {
									okCB(generateLayerContent(t, processed, &types.EmptyBlockID), peer, numPeers)
								} else {
									okCB(generateLayerContent(t, processed), peer, numPeers)
								}
							}
						} else {
							errCB(errors.New("not available"), peer, numPeers)
						}
					}
					return nil
				})
			tl.mCertH.EXPECT().HandleSyncedCertificate(gomock.Any(), layerID, gomock.Any()).Return(nil).AnyTimes()
			if tc.zeroBlock {
				tl.mMesh.EXPECT().SetZeroBlockLayer(gomock.Any(), layerID)
			} else {
				tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BallotDB, false).Return(nil)
				tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BlockDB, false).Return(nil)
			}

			res := <-tl.PollLayerContent(context.TODO(), layerID)
			assert.Nil(t, res.Err)
			assert.Equal(t, layerID, res.Layer)
			got, err := layers.GetHareOutput(tl.db, layerID)
			require.NoError(t, err)
			if tc.emptyHareOutput {
				require.Equal(t, types.EmptyBlockID, got)
			} else {
				require.NotEqual(t, types.EmptyBlockID, got)
			}
		})
	}
}

func TestPollLayerContent_MissingBlocks(t *testing.T) {
	requested := types.NewLayerID(20)
	blks := &layerData{
		Blocks:         []types.BlockID{{1, 1, 1}, {2, 2, 2}, {3, 3, 3}},
		ProcessedLayer: requested,
		HareOutput: &types.Certificate{
			BlockID: types.EmptyBlockID,
		},
	}
	data, err := codec.Encode(blks)
	require.NoError(t, err)
	tl := createTestLogic(t)
	numPeers := 2
	peers := genPeers(numPeers)
	tl.mFetcher.EXPECT().GetLayerData(gomock.Any(), requested, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, okCB func([]byte, p2p.Peer, int), errCB func(error, p2p.Peer, int)) error {
			for _, peer := range peers {
				tl.mFetcher.EXPECT().RegisterPeerHashes(peer, gomock.Any())
				okCB(data, peer, numPeers)
			}
			return nil
		})
	tl.mCertH.EXPECT().HandleSyncedCertificate(gomock.Any(), requested, gomock.Any()).Return(nil).AnyTimes()

	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BallotDB, false).Return(nil).AnyTimes()
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BlockDB, false).DoAndReturn(
		func(hashes []types.Hash32, _ datastore.Hint, _ bool) map[types.Hash32]chan ftypes.HashDataPromiseResult {
			rst := map[types.Hash32]chan ftypes.HashDataPromiseResult{}
			for _, hash := range hashes {
				rst[hash] = make(chan ftypes.HashDataPromiseResult, 1)
				rst[hash] <- ftypes.HashDataPromiseResult{
					Hash: hash,
					Err:  errors.New("failed request"),
				}
			}
			return rst
		},
	).Times(1)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BlockDB, false).DoAndReturn(
		func(hashes []types.Hash32, _ datastore.Hint, _ bool) map[types.Hash32]chan ftypes.HashDataPromiseResult {
			return nil
		},
	).AnyTimes()

	res := <-tl.PollLayerContent(context.TODO(), requested)
	assert.Nil(t, res.Err)
	got, err := layers.GetHareOutput(tl.db, requested)
	require.NoError(t, err)
	require.Equal(t, types.EmptyBlockID, got)
}

func TestPollLayerContent_SameProcessedLayer(t *testing.T) {
	layerID := types.NewLayerID(10)
	tl := createTestLogic(t)
	numPeers := 4
	peers := genPeers(numPeers)
	tl.mFetcher.EXPECT().GetLayerData(gomock.Any(), layerID, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, okCB func([]byte, p2p.Peer, int), errCB func(error, p2p.Peer, int)) error {
			for i, peer := range peers {
				tl.mFetcher.EXPECT().RegisterPeerHashes(peer, gomock.Any())
				if i == 0 {
					okCB(generateLayerContent(t, layerID), peer, numPeers)
				} else {
					okCB(generateLayerContent(t, layerID, &types.EmptyBlockID), peer, numPeers)
				}
			}
			return nil
		})
	tl.mCertH.EXPECT().HandleSyncedCertificate(gomock.Any(), layerID, gomock.Any()).Return(nil).AnyTimes()

	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BallotDB, false).Return(nil).Times(numPeers)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BlockDB, false).Return(nil).Times(numPeers)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
	got, err := layers.GetHareOutput(tl.db, layerID)
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyBlockID, got)
}

func TestPollLayerContent_HigherProcessedLayerWins(t *testing.T) {
	layerID := types.NewLayerID(10)
	tl := createTestLogic(t)
	numPeers := 4
	peers := genPeers(numPeers)
	tl.mFetcher.EXPECT().GetLayerData(gomock.Any(), layerID, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, okCB func([]byte, p2p.Peer, int), errCB func(error, p2p.Peer, int)) error {
			for i, peer := range peers {
				tl.mFetcher.EXPECT().RegisterPeerHashes(peer, gomock.Any())
				if i == 0 {
					okCB(generateLayerContent(t, layerID.Add(1), &types.EmptyBlockID), peer, numPeers)
				} else {
					okCB(generateLayerContent(t, layerID), peer, numPeers)
				}
			}
			return nil
		})
	tl.mCertH.EXPECT().HandleSyncedCertificate(gomock.Any(), layerID, gomock.Any()).Return(nil).AnyTimes()

	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BallotDB, false).Return(nil).Times(numPeers)
	tl.mFetcher.EXPECT().GetHashes(gomock.Any(), datastore.BlockDB, false).Return(nil).Times(numPeers)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
	got, err := layers.GetHareOutput(tl.db, layerID)
	require.NoError(t, err)
	require.Equal(t, types.EmptyBlockID, got)
	gotC, err := layers.GetCert(tl.db, layerID)
	require.NoError(t, err)
	require.Equal(t, types.EmptyBlockID, gotC.BlockID)
}

func TestPollLayerContent_FailureToSaveZeroBlockLayerIgnored(t *testing.T) {
	layerID := types.NewLayerID(10)
	tl := createTestLogic(t)
	numPeers := 4
	peers := genPeers(numPeers)
	tl.mFetcher.EXPECT().GetLayerData(gomock.Any(), layerID, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ types.LayerID, okCB func([]byte, p2p.Peer, int), errCB func(error, p2p.Peer, int)) error {
			for _, peer := range peers {
				okCB(generateEmptyLayer(), peer, numPeers)
			}
			return nil
		})
	tl.mCertH.EXPECT().HandleSyncedCertificate(gomock.Any(), layerID, gomock.Any()).Return(nil).AnyTimes()
	tl.mMesh.EXPECT().SetZeroBlockLayer(gomock.Any(), layerID).Return(errors.New("whatever")).Times(1)

	res := <-tl.PollLayerContent(context.TODO(), layerID)
	assert.NoError(t, res.Err)
	assert.Equal(t, layerID, res.Layer)
	got, err := layers.GetHareOutput(tl.db, layerID)
	require.NoError(t, err)
	require.Equal(t, types.EmptyBlockID, got)
	gotC, err := layers.GetCert(tl.db, layerID)
	require.NoError(t, err)
	require.Equal(t, types.EmptyBlockID, gotC.BlockID)
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
			l := createTestLogic(t)
			results := make(map[types.Hash32]chan ftypes.HashDataPromiseResult, len(hashes))
			for i, h := range hashes {
				ch := make(chan ftypes.HashDataPromiseResult, 1)
				if tc.fetchErrs[i] == nil {
					data, err := codec.Encode(blks[i])
					require.NoError(t, err)
					ch <- ftypes.HashDataPromiseResult{
						Hash: h,
						Data: data,
					}
					l.mBlocksH.EXPECT().HandleSyncedBlock(gomock.Any(), data).Return(tc.hdlrErr)
				} else {
					ch <- ftypes.HashDataPromiseResult{
						Hash: h,
						Err:  tc.fetchErrs[i],
					}
				}
				results[h] = ch
			}

			l.mFetcher.EXPECT().GetHashes(hashes, datastore.BlockDB, false).Return(results)
			require.ErrorIs(t, l.GetBlocks(context.TODO(), blockIDs), tc.err)
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
			l := createTestLogic(t)
			results := make(map[types.Hash32]chan ftypes.HashDataPromiseResult, len(hashes))
			for i, h := range hashes {
				ch := make(chan ftypes.HashDataPromiseResult, 1)
				if tc.fetchErrs[i] == nil {
					data, err := codec.Encode(blts[i])
					require.NoError(t, err)
					ch <- ftypes.HashDataPromiseResult{
						Hash: h,
						Data: data,
					}
					l.mBallotH.EXPECT().HandleSyncedBallot(gomock.Any(), data).Return(tc.hdlrErr)
				} else {
					ch <- ftypes.HashDataPromiseResult{
						Hash: h,
						Err:  tc.fetchErrs[i],
					}
				}
				results[h] = ch
			}

			l.mFetcher.EXPECT().GetHashes(hashes, datastore.BallotDB, false).Return(results)
			require.ErrorIs(t, l.GetBallots(context.TODO(), ballotIDs), tc.err)
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
			l := createTestLogic(t)
			results := make(map[types.Hash32]chan ftypes.HashDataPromiseResult, len(hashes))
			for i, h := range hashes {
				ch := make(chan ftypes.HashDataPromiseResult, 1)
				if tc.fetchErrs[i] == nil {
					data, err := codec.Encode(proposals[i])
					require.NoError(t, err)
					ch <- ftypes.HashDataPromiseResult{
						Hash: h,
						Data: data,
					}
					l.mProposalH.EXPECT().HandleSyncedProposal(gomock.Any(), data).Return(tc.hdlrErr)
				} else {
					ch <- ftypes.HashDataPromiseResult{
						Hash: h,
						Err:  tc.fetchErrs[i],
					}
				}
				results[h] = ch
			}

			l.mFetcher.EXPECT().GetHashes(hashes, datastore.ProposalDB, false).Return(results)
			require.ErrorIs(t, l.GetProposals(context.TODO(), proposalIDs), tc.err)
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
			l := createTestLogic(t).withMethod(tc.method)
			txs := genTransactions(t, 19)
			tids := types.ToTransactionIDs(txs)
			hashes := types.TransactionIDsToHashes(tids)

			errUnknown := errors.New("unknown")
			results := make(map[types.Hash32]chan ftypes.HashDataPromiseResult, len(hashes))
			for i, h := range hashes {
				ch := make(chan ftypes.HashDataPromiseResult, 1)
				if i == 0 {
					ch <- ftypes.HashDataPromiseResult{
						Hash: h,
						Err:  errUnknown,
					}
				} else {
					data, err := codec.Encode(tids[i])
					require.NoError(t, err)
					ch <- ftypes.HashDataPromiseResult{
						Hash: h,
						Data: data,
					}
					l.expectTransactionCall(data).Return(nil).Times(1)
				}
				results[h] = ch
			}

			l.mFetcher.EXPECT().GetHashes(hashes, datastore.TXDB, false).Return(results).Times(1)
			assert.ErrorIs(t, l.getTxs(tids), errUnknown)
		})
	}
}

func TestGetTxs_HandlerError(t *testing.T) {
	l := createTestLogic(t)
	txs := genTransactions(t, 19)
	tids := types.ToTransactionIDs(txs)
	hashes := types.TransactionIDsToHashes(tids)

	errUnknown := errors.New("unknown")
	results := make(map[types.Hash32]chan ftypes.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan ftypes.HashDataPromiseResult, 1)
		data, err := codec.Encode(tids[i])
		require.NoError(t, err)
		ch <- ftypes.HashDataPromiseResult{
			Hash: h,
			Data: data,
		}
		results[h] = ch
		l.mTxH.EXPECT().HandleBlockTransaction(gomock.Any(), data).Return(errUnknown).Times(1)
	}

	l.mFetcher.EXPECT().GetHashes(hashes, datastore.TXDB, false).Return(results).Times(1)
	assert.ErrorIs(t, l.GetBlockTxs(context.TODO(), tids), errUnknown)
}

func TestGetTxs(t *testing.T) {
	l := createTestLogic(t)
	txs := genTransactions(t, 19)
	tids := types.ToTransactionIDs(txs)
	hashes := types.TransactionIDsToHashes(tids)

	results := make(map[types.Hash32]chan ftypes.HashDataPromiseResult, len(hashes))
	for i, h := range hashes {
		ch := make(chan ftypes.HashDataPromiseResult, 1)
		data, err := codec.Encode(tids[i])
		require.NoError(t, err)
		ch <- ftypes.HashDataPromiseResult{
			Hash: h,
			Data: data,
		}
		results[h] = ch
		l.mTxH.EXPECT().HandleBlockTransaction(gomock.Any(), data).Return(nil).Times(1)
	}

	l.mFetcher.EXPECT().GetHashes(hashes, datastore.TXDB, false).Return(results).Times(1)
	assert.NoError(t, l.GetBlockTxs(context.TODO(), tids))
}

func genATXs(t *testing.T, num int) []*types.ActivationTx {
	t.Helper()
	atxs := make([]*types.ActivationTx, 0, num)
	for i := 0; i < num; i++ {
		atx := types.NewActivationTx(types.NIPostChallenge{}, types.Address{1, 2, 3}, &types.NIPost{}, uint(i), nil)
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
			l := createTestLogic(t)
			results := make(map[types.Hash32]chan ftypes.HashDataPromiseResult, len(hashes))
			for i, h := range hashes {
				ch := make(chan ftypes.HashDataPromiseResult, 1)
				if tc.fetchErrs[i] == nil {
					data, err := codec.Encode(atxs[i])
					require.NoError(t, err)
					ch <- ftypes.HashDataPromiseResult{
						Hash: h,
						Data: data,
					}
					l.mAtxH.EXPECT().HandleAtxData(gomock.Any(), data).Return(tc.hdlrErr)
				} else {
					ch <- ftypes.HashDataPromiseResult{
						Hash: h,
						Err:  tc.fetchErrs[i],
					}
				}
				results[h] = ch
			}

			l.mFetcher.EXPECT().GetHashes(hashes, datastore.ATXDB, false).Return(results)
			require.ErrorIs(t, l.GetAtxs(context.TODO(), atxIDs), tc.err)
		})
	}
}

func TestGetPoetProof(t *testing.T) {
	l := createTestLogic(t)
	proof := types.PoetProofMessage{}
	h := types.RandomHash()

	ch := make(chan ftypes.HashDataPromiseResult, 1)
	data, err := codec.Encode(proof)
	require.NoError(t, err)
	ch <- ftypes.HashDataPromiseResult{
		Hash: h,
		Data: data,
	}

	l.mFetcher.EXPECT().GetHash(h, datastore.POETDB, false).Return(ch).Times(1)
	l.mPoetH.EXPECT().ValidateAndStoreMsg(data).Return(nil).Times(1)
	assert.NoError(t, l.GetPoetProof(context.TODO(), h))

	ch <- ftypes.HashDataPromiseResult{
		Hash: h,
		Data: data,
	}
	l.mFetcher.EXPECT().GetHash(h, datastore.POETDB, false).Return(ch).Times(1)
	l.mPoetH.EXPECT().ValidateAndStoreMsg(data).Return(sql.ErrObjectExists).Times(1)
	assert.NoError(t, l.GetPoetProof(context.TODO(), h))

	ch <- ftypes.HashDataPromiseResult{
		Hash: h,
		Data: data,
	}
	l.mFetcher.EXPECT().GetHash(h, datastore.POETDB, false).Return(ch).Times(1)
	l.mPoetH.EXPECT().ValidateAndStoreMsg(data).Return(errors.New("unknown")).Times(1)
	assert.Error(t, l.GetPoetProof(context.TODO(), h))
}
