package proposals

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/proposals/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

const (
	layersPerEpoch = uint32(3)
	layerAvgSize   = uint32(10)
)

func genGoldenATXID() types.ATXID {
	return types.ATXID(types.HexToHash32("0x6666666"))
}

type mockSet struct {
	mf  *smocks.MockFetcher
	mbc *smocks.MockBeaconCollector
	mm  *mocks.MockmeshProvider
	mv  *mocks.MockeligibilityValidator
	md  *mocks.MockballotDecoder
}

func (ms *mockSet) decodeAnyBallots() *mockSet {
	ms.md.EXPECT().DecodeBallot(gomock.Any()).AnyTimes()
	ms.md.EXPECT().StoreBallot(gomock.Any()).AnyTimes()
	return ms
}

type testHandler struct {
	*Handler
	*mockSet
}

func fullMockSet(tb testing.TB) *mockSet {
	ctrl := gomock.NewController(tb)
	return &mockSet{
		mf:  smocks.NewMockFetcher(ctrl),
		mbc: smocks.NewMockBeaconCollector(ctrl),
		mm:  mocks.NewMockmeshProvider(ctrl),
		mv:  mocks.NewMockeligibilityValidator(ctrl),
		md:  mocks.NewMockballotDecoder(ctrl),
	}
}

func createTestHandler(t *testing.T) *testHandler {
	types.SetLayersPerEpoch(layersPerEpoch)
	ms := fullMockSet(t)
	return &testHandler{
		Handler: NewHandler(datastore.NewCachedDB(sql.InMemory(), logtest.New(t)), ms.mf, ms.mbc, ms.mm, ms.md,
			WithLogger(logtest.New(t)),
			WithConfig(Config{
				LayerSize:      layerAvgSize,
				LayersPerEpoch: layersPerEpoch,
				GoldenATXID:    genGoldenATXID(),
				MaxExceptions:  3,
				Hdist:          5,
			}),
			withValidator(ms.mv)),
		mockSet: ms,
	}
}

func createTestHandlerNoopDecoder(t *testing.T) *testHandler {
	th := createTestHandler(t)
	th.mockSet.decodeAnyBallots()
	return th
}

type createOpt any

type createBallotOpt func(b *types.Ballot)

func withSupportBlocks(blocks ...*types.Block) createBallotOpt {
	return func(b *types.Ballot) {
		b.Votes.Support = nil
		for _, block := range blocks {
			b.Votes.Support = append(b.Votes.Support, block.ToVote())
		}
	}
}

func withAgainstBlocks(blocks ...*types.Block) createBallotOpt {
	return func(b *types.Ballot) {
		b.Votes.Against = nil
		for _, block := range blocks {
			b.Votes.Against = append(b.Votes.Against, block.ToVote())
		}
	}
}

func withAbstain(layers ...types.LayerID) createBallotOpt {
	return func(b *types.Ballot) {
		b.Votes.Abstain = layers
	}
}

func withLayer(lid types.LayerID) createBallotOpt {
	return func(b *types.Ballot) {
		b.LayerIndex = lid
	}
}

func withAnyRefData() createBallotOpt {
	return func(b *types.Ballot) {
		b.RefBallot = types.EmptyBallotID
		b.EpochData = &types.EpochData{
			ActiveSet: types.ATXIDList{types.RandomATXID(), types.RandomATXID()},
			Beacon:    types.RandomBeacon(),
		}
	}
}

type createProposalOpt func(p *types.Proposal)

func withTransactions(ids ...types.TransactionID) createProposalOpt {
	return func(p *types.Proposal) {
		p.TxIDs = ids
	}
}

func createProposal(t *testing.T, opts ...any) *types.Proposal {
	t.Helper()
	b := types.RandomBallot()
	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: *b,
			TxIDs:  []types.TransactionID{types.RandomTransactionID(), types.RandomTransactionID()},
		},
	}
	for _, opt := range opts {
		switch unwrap := opt.(type) {
		case createBallotOpt:
			unwrap(&p.Ballot)
		case createProposalOpt:
			unwrap(p)
		}
	}
	signer := signing.NewEdSigner()
	p.Ballot.Signature = signer.Sign(p.Ballot.SignedBytes())
	p.Signature = signer.Sign(p.Bytes())
	require.NoError(t, p.Initialize())
	return p
}

func encodeProposal(t *testing.T, p *types.Proposal) []byte {
	t.Helper()
	data, err := codec.Encode(p)
	require.NoError(t, err)
	return data
}

func createBallot(t *testing.T, opts ...createBallotOpt) *types.Ballot {
	t.Helper()
	b := types.RandomBallot()
	for _, opt := range opts {
		opt(b)
	}
	return signAndInit(t, b)
}

func signAndInit(t *testing.T, b *types.Ballot) *types.Ballot {
	t.Helper()
	b.Signature = signing.NewEdSigner().Sign(b.SignedBytes())
	require.NoError(t, b.Initialize())
	return b
}

func createRefBallot(t *testing.T) *types.Ballot {
	t.Helper()
	b := types.RandomBallot()
	b.RefBallot = types.EmptyBallotID
	b.EpochData = &types.EpochData{
		ActiveSet: types.ATXIDList{types.RandomATXID(), types.RandomATXID()},
		Beacon:    types.RandomBeacon(),
	}
	return b
}

func encodeBallot(t *testing.T, b *types.Ballot) []byte {
	t.Helper()
	data, err := codec.Encode(b)
	require.NoError(t, err)
	return data
}

func checkProposal(t *testing.T, cdb *datastore.CachedDB, p *types.Proposal, exist bool) {
	t.Helper()
	got, err := proposals.Get(cdb, p.ID())
	if exist {
		require.NoError(t, err)
		require.Equal(t, p, got)
	} else {
		require.ErrorIs(t, err, sql.ErrNotFound)
	}
}

func checkIdentity(t *testing.T, cdb *datastore.CachedDB, b *types.Ballot, malicious bool) {
	t.Helper()
	gotM, err := identities.IsMalicious(cdb, b.SmesherID().Bytes())
	require.NoError(t, err)
	require.Equal(t, malicious, gotM)
}

func TestBallot_MalformedData(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := createBallot(t)
	data, err := codec.Encode(&b.InnerBallot)
	require.NoError(t, err)
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errMalformedData)
}

func TestBallot_BadSignature(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := createBallot(t)
	b.Signature = b.Signature[1:]
	data := encodeBallot(t, b)
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errInitialize)
}

func TestBallot_KnownBallot(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := createBallot(t)
	require.NoError(t, ballots.Add(th.cdb, b))
	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	require.NoError(t, th.HandleSyncedBallot(context.TODO(), data))
}

func TestBallot_EmptyATXID(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := types.RandomBallot()
	b.AtxID = *types.EmptyATXID
	b = signAndInit(t, b)
	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errInvalidATXID)
}

func TestBallot_GoldenATXID(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := types.RandomBallot()
	b.AtxID = genGoldenATXID()
	b = signAndInit(t, b)
	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errInvalidATXID)
}

func TestBallot_RefBallotMissingEpochData(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := createRefBallot(t)
	b.EpochData = nil
	signAndInit(t, b)
	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errMissingEpochData)
}

func TestBallot_RefBallotMissingBeacon(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := createRefBallot(t)
	b.EpochData.Beacon = types.EmptyBeacon
	signAndInit(t, b)
	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errMissingBeacon)
}

func TestBallot_RefBallotEmptyActiveSet(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := createRefBallot(t)
	b.EpochData.ActiveSet = nil
	signAndInit(t, b)
	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errEmptyActiveSet)
}

func TestBallot_RefBallotDuplicateInActiveSet(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := createRefBallot(t)
	b.EpochData.ActiveSet = append(b.EpochData.ActiveSet, b.EpochData.ActiveSet[0])
	signAndInit(t, b)
	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errDuplicateATX)
}

func TestBallot_NotRefBallotButHasEpochData(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := types.RandomBallot()
	b.EpochData = &types.EpochData{}
	b = signAndInit(t, b)
	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errUnexpectedEpochData)
}

func TestBallot_BallotDoubleVotedWithinHdist(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.NewLayerID(100)
	cutoff := lid.Sub(th.cfg.Hdist)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: cutoff.Add(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: cutoff.Add(1)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), types.ToBlockIDs(supported)).Return(nil).Times(1)
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errDoubleVoting)
	checkIdentity(t, th.cdb, b, true)
}

func TestBallot_BallotDoubleVotedWithinHdist_LyrBfrHdist(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.NewLayerID(100)
	th.cfg.Hdist = lid.Add(1).Uint32()
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)

	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), types.ToBlockIDs(supported)).Return(nil).Times(1)
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errDoubleVoting)
	checkIdentity(t, th.cdb, b, true)
}

func TestBallot_BallotDoubleVotedOutsideHdist(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.NewLayerID(100)
	cutoff := lid.Sub(th.cfg.Hdist)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: cutoff.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: cutoff.Sub(1)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)

	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), types.ToBlockIDs(supported)).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(b).Return(nil)
	require.NoError(t, th.HandleSyncedBallot(context.TODO(), data))
	checkIdentity(t, th.cdb, b, false)
}

func TestBallot_ConflictingForAndAgainst(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.NewLayerID(100)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
		withAgainstBlocks(supported...),
	)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), types.ToBlockIDs(append(supported, supported...))).Return(nil).Times(1)
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errConflictingExceptions)
	checkIdentity(t, th.cdb, b, true)
}

func TestBallot_ConflictingForAndAbstain(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.NewLayerID(100)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
		withAbstain(lid.Sub(1)),
	)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), types.ToBlockIDs(supported)).Return(nil).Times(1)
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errConflictingExceptions)
	checkIdentity(t, th.cdb, b, true)
}

func TestBallot_ConflictingAgainstAndAbstain(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.NewLayerID(100)
	against := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(),
		withAgainstBlocks(against...),
		withAbstain(lid.Sub(1)),
	)
	for _, blk := range against {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), types.ToBlockIDs(against)).Return(nil).Times(1)
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errConflictingExceptions)
	checkIdentity(t, th.cdb, b, true)
}

func TestBallot_ExceedMaxExceptions(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.NewLayerID(100)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
		types.NewExistingBlock(types.BlockID{3}, types.InnerBlock{LayerIndex: lid.Sub(3)}),
		types.NewExistingBlock(types.BlockID{4}, types.InnerBlock{LayerIndex: lid.Sub(4)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), types.ToBlockIDs(supported)).Return(nil).Times(1)
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errExceptionsOverflow)
	checkIdentity(t, th.cdb, b, false)
}

func TestBallot_BallotsNotAvailable(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := createBallot(t)
	data := encodeBallot(t, b)

	errUnknown := errors.New("unknown")
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(errUnknown).Times(1)
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errUnknown)
	checkIdentity(t, th.cdb, b, false)
}

func TestBallot_ATXsNotAvailable(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	b := createBallot(t)
	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	errUnknown := errors.New("unknown")
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(errUnknown).Times(1)
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errUnknown)
	checkIdentity(t, th.cdb, b, false)
}

func TestBallot_BlocksNotAvailable(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.NewLayerID(100)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	errUnknown := errors.New("unknown")
	th.mf.EXPECT().GetBlocks(gomock.Any(), types.ToBlockIDs(supported)).Return(errUnknown).Times(1)
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errUnknown)
	checkIdentity(t, th.cdb, b, false)
}

func TestBallot_ErrorCheckingEligible(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.NewLayerID(100)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), toIds(b.Votes.Support)).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return false, errors.New("unknown")
		})
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errNotEligible)
	checkIdentity(t, th.cdb, b, false)
}

func TestBallot_NotEligible(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.NewLayerID(100)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), toIds(b.Votes.Support)).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return false, nil
		})
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errNotEligible)
	checkIdentity(t, th.cdb, b, false)
}

func TestBallot_InvalidVote(t *testing.T) {
	t.Run("support", func(t *testing.T) {
		th := createTestHandlerNoopDecoder(t)
		lid := types.NewLayerID(100)
		supported := []*types.Block{
			types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
			types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
		}
		b := createBallot(t,
			withLayer(lid),
			withSupportBlocks(supported...),
		)
		for _, blk := range supported {
			blk.TickHeight = 100
			require.NoError(t, blocks.Add(th.cdb, blk))
		}
		data := encodeBallot(t, b)
		th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
		th.mf.EXPECT().GetBallots(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		th.mf.EXPECT().GetAtxs(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		th.mf.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errInvalidVote)
		checkIdentity(t, th.cdb, b, false)
	})
	t.Run("against", func(t *testing.T) {
		th := createTestHandlerNoopDecoder(t)
		lid := types.NewLayerID(100)
		against := []*types.Block{
			types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
			types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
		}
		b := createBallot(t,
			withLayer(lid),
			withSupportBlocks(),
			withAgainstBlocks(against...),
		)
		for _, blk := range against {
			blk.TickHeight = 100
			require.NoError(t, blocks.Add(th.cdb, blk))
		}
		data := encodeBallot(t, b)
		th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
		th.mf.EXPECT().GetBallots(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		th.mf.EXPECT().GetAtxs(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		th.mf.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), errInvalidVote)
		checkIdentity(t, th.cdb, b, false)
	})
}

func TestBallot_Success(t *testing.T) {
	th := createTestHandler(t)
	lid := types.NewLayerID(100)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), toIds(b.Votes.Support)).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(b).Return(nil)
	decoded := &tortoise.DecodedBallot{Ballot: b}
	th.md.EXPECT().DecodeBallot(b).Return(decoded, nil)
	th.md.EXPECT().StoreBallot(decoded).Return(nil)
	require.NoError(t, th.HandleSyncedBallot(context.TODO(), data))
	checkIdentity(t, th.cdb, b, false)
}

func TestBallot_RefBallot(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.NewLayerID(100)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	b := createBallot(t,
		withLayer(lid),
		withSupportBlocks(supported...),
		withAnyRefData(),
	)
	for _, blk := range supported {
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base}).Return(nil).Times(1)
	atxIDs := types.ATXIDList{b.AtxID}
	atxIDs = append(atxIDs, b.EpochData.ActiveSet...)
	th.mf.EXPECT().GetAtxs(gomock.Any(), atxIDs).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), toIds(b.Votes.Support)).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(b).Return(nil)
	require.NoError(t, th.HandleSyncedBallot(context.TODO(), data))
	checkIdentity(t, th.cdb, b, false)
}

func TestBallot_DecodeBeforeVotesConsistency(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	b.Votes.Against = b.Votes.Support
	for _, bid := range b.Votes.Support {
		blk := types.NewExistingBlock(bid.ID, types.InnerBlock{LayerIndex: b.LayerIndex.Sub(1)})
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	expected := errors.New("test")

	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).Return(nil).Times(1)

	decoded := &tortoise.DecodedBallot{Ballot: b}
	th.md.EXPECT().DecodeBallot(b).Return(decoded, expected)
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), expected)
}

func TestBallot_DecodedStoreFailure(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	b.Votes.Support = nil // just to avoid creating blocks
	expected := errors.New("test")

	data := encodeBallot(t, b)
	th.mf.EXPECT().AddPeersFromHash(b.ID().AsHash32(), collectHashes(*b))
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).Return(true, nil).Times(1)

	decoded := &tortoise.DecodedBallot{Ballot: b}
	th.md.EXPECT().DecodeBallot(b).Return(decoded, nil)
	th.mm.EXPECT().AddBallot(b).Return(nil)
	th.md.EXPECT().StoreBallot(decoded).Return(expected)
	require.ErrorIs(t, th.HandleSyncedBallot(context.TODO(), data), expected)
}

func TestProposal_MalformedData(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	p := createProposal(t)
	data, err := codec.Encode(&p.InnerProposal)
	require.NoError(t, err)
	require.ErrorIs(t, th.HandleSyncedProposal(context.TODO(), data), errMalformedData)
	require.Equal(t, pubsub.ValidationReject, th.HandleProposal(context.TODO(), "", data))
	checkProposal(t, th.cdb, p, false)
}

func TestProposal_BadSignature(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	p := createProposal(t)
	p.Signature = p.Signature[1:]
	data := encodeProposal(t, p)
	require.ErrorIs(t, th.HandleSyncedProposal(context.TODO(), data), errInitialize)
	require.Equal(t, pubsub.ValidationIgnore, th.HandleProposal(context.TODO(), "", data))
	checkProposal(t, th.cdb, p, false)
}

func TestProposal_KnownProposal(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	p := createProposal(t)
	require.NoError(t, ballots.Add(th.cdb, &p.Ballot))
	require.NoError(t, proposals.Add(th.cdb, p))
	data := encodeProposal(t, p)
	require.NoError(t, th.HandleSyncedProposal(context.TODO(), data))
	require.Equal(t, pubsub.ValidationIgnore, th.HandleProposal(context.TODO(), "", data))
	checkProposal(t, th.cdb, p, true)
}

func TestProposal_DuplicateTXs(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.NewLayerID(100)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	tid := types.RandomTransactionID()
	p := createProposal(t,
		withLayer(lid),
		withSupportBlocks(supported...),
		withTransactions(tid, tid),
	)
	for _, block := range supported {
		require.NoError(t, blocks.Add(th.cdb, block))
	}
	data := encodeProposal(t, p)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), toIds(p.Votes.Support)).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(nil).DoAndReturn(
		func(got *types.Ballot) error {
			require.NoError(t, ballots.Add(th.cdb, got))
			return nil
		})
	th.mf.EXPECT().RegisterPeerHashes(p2p.NoPeer, collectHashes(*p))
	require.ErrorIs(t, th.HandleSyncedProposal(context.TODO(), data), errDuplicateTX)
	checkProposal(t, th.cdb, p, false)
}

func TestProposal_TXsNotAvailable(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.NewLayerID(100)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	p := createProposal(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	for _, block := range supported {
		require.NoError(t, blocks.Add(th.cdb, block))
	}
	data := encodeProposal(t, p)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), toIds(p.Votes.Support)).Return(nil).Times(1)
	th.mf.EXPECT().RegisterPeerHashes(p2p.NoPeer, collectHashes(*p))
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(nil).DoAndReturn(
		func(got *types.Ballot) error {
			require.NoError(t, ballots.Add(th.cdb, got))
			return nil
		})

	errUnknown := errors.New("unknown")
	th.mf.EXPECT().GetProposalTxs(gomock.Any(), p.TxIDs).Return(errUnknown).Times(1)
	require.ErrorIs(t, th.HandleSyncedProposal(context.TODO(), data), errUnknown)
	checkProposal(t, th.cdb, p, false)
}

func TestProposal_FailedToAddProposalTXs(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.NewLayerID(100)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	p := createProposal(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	for _, block := range supported {
		require.NoError(t, blocks.Add(th.cdb, block))
	}
	data := encodeProposal(t, p)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), toIds(p.Votes.Support)).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(nil).DoAndReturn(
		func(got *types.Ballot) error {
			require.NoError(t, ballots.Add(th.cdb, got))
			return nil
		})
	th.mf.EXPECT().GetProposalTxs(gomock.Any(), p.TxIDs).Return(nil).Times(1)
	th.mf.EXPECT().RegisterPeerHashes(p2p.NoPeer, collectHashes(*p))
	errUnknown := errors.New("unknown")
	th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(errUnknown).Times(1)
	require.ErrorIs(t, th.HandleSyncedProposal(context.TODO(), data), errUnknown)
	checkProposal(t, th.cdb, p, true)
}

func TestProposal_ProposalGossip_Concurrent(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.NewLayerID(100)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	p := createProposal(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	for _, block := range supported {
		require.NoError(t, blocks.Add(th.cdb, block))
	}
	data := encodeProposal(t, p)

	th.mf.EXPECT().RegisterPeerHashes(p2p.NoPeer, collectHashes(*p)).MinTimes(1).MaxTimes(2)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}).Return(nil).MinTimes(1).MaxTimes(2)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).MinTimes(1).MaxTimes(2)
	th.mf.EXPECT().GetBlocks(gomock.Any(), toIds(p.Votes.Support)).Return(nil).MinTimes(1).MaxTimes(2)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		}).MinTimes(1).MaxTimes(2)
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(nil).DoAndReturn(
		func(got *types.Ballot) error {
			_ = ballots.Add(th.cdb, got)
			return nil
		}).MinTimes(1).MaxTimes(2)
	th.mf.EXPECT().GetProposalTxs(gomock.Any(), p.TxIDs).Return(nil).MinTimes(1).MaxTimes(2)
	th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(nil).Times(1)

	var wg sync.WaitGroup
	wg.Add(2)
	var res1, res2 pubsub.ValidationResult
	go func() {
		defer wg.Done()
		res1 = th.HandleProposal(context.TODO(), p2p.NoPeer, data)
	}()
	go func() {
		defer wg.Done()
		res2 = th.HandleProposal(context.TODO(), p2p.NoPeer, data)
	}()
	wg.Wait()
	if res1 == pubsub.ValidationAccept {
		require.Equal(t, pubsub.ValidationIgnore, res2)
	} else {
		require.Equal(t, pubsub.ValidationIgnore, res1)
		require.Equal(t, pubsub.ValidationAccept, res2)
	}
	checkProposal(t, th.cdb, p, true)
}

func TestProposal_ProposalGossip_Fetched(t *testing.T) {
	tests := []struct {
		name        string
		propFetched bool
	}{
		{
			name:        "proposal fetched",
			propFetched: true,
		},
		{
			name: "ballot fetched",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			th := createTestHandlerNoopDecoder(t)
			lid := types.NewLayerID(100)
			supported := []*types.Block{
				types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
				types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
			}
			p := createProposal(t,
				withLayer(lid),
				withSupportBlocks(supported...),
			)
			for _, block := range supported {
				require.NoError(t, blocks.Add(th.cdb, block))
			}
			data := encodeProposal(t, p)

			th.mf.EXPECT().RegisterPeerHashes(p2p.NoPeer, collectHashes(*p))
			th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}).Return(nil)
			th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil)
			th.mf.EXPECT().GetBlocks(gomock.Any(), toIds(p.Votes.Support)).Return(nil)
			th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
				func(ctx context.Context, ballot *types.Ballot) (bool, error) {
					require.Equal(t, p.Ballot.ID(), ballot.ID())
					if tc.propFetched {
						// a separate goroutine fetched the propFetched and saved it to database
						require.NoError(t, proposals.Add(th.cdb, p))
						require.NoError(t, ballots.Add(th.cdb, &p.Ballot))
					} else {
						// a separate goroutine fetched the ballot and saved it to database
						require.NoError(t, ballots.Add(th.cdb, &p.Ballot))
					}
					return true, nil
				})
			th.mm.EXPECT().AddBallot(&p.Ballot).Return(nil).Return(nil)
			th.mf.EXPECT().GetProposalTxs(gomock.Any(), p.TxIDs).Return(nil)
			if tc.propFetched {
				require.Equal(t, pubsub.ValidationIgnore, th.HandleProposal(context.TODO(), p2p.NoPeer, data))
			} else {
				th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(nil).Times(1)
				require.Equal(t, pubsub.ValidationAccept, th.HandleProposal(context.TODO(), p2p.NoPeer, data))
			}
			checkProposal(t, th.cdb, p, true)
			checkIdentity(t, th.cdb, &p.Ballot, false)
		})
	}
}

func TestProposal_ValidProposal(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.NewLayerID(10)
	blks := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	p := createProposal(t,
		withLayer(lid),
		withSupportBlocks(blks...),
	)
	for _, block := range blks {
		require.NoError(t, blocks.Add(th.cdb, block))
	}
	data := encodeProposal(t, p)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), toIds(p.Votes.Support)).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(nil).DoAndReturn(
		func(got *types.Ballot) error {
			require.NoError(t, ballots.Add(th.cdb, got))
			return nil
		})
	th.mf.EXPECT().GetProposalTxs(gomock.Any(), p.TxIDs).Return(nil).Times(1)
	th.mf.EXPECT().RegisterPeerHashes(p2p.NoPeer, collectHashes(*p))
	th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(nil).Times(1)
	require.NoError(t, th.HandleSyncedProposal(context.TODO(), data))
	checkProposal(t, th.cdb, p, true)
}

func TestMetrics(t *testing.T) {
	th := createTestHandlerNoopDecoder(t)
	lid := types.NewLayerID(100)
	supported := []*types.Block{
		types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{LayerIndex: lid.Sub(1)}),
		types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{LayerIndex: lid.Sub(2)}),
	}
	p := createProposal(t,
		withLayer(lid),
		withSupportBlocks(supported...),
	)
	for _, block := range supported {
		require.NoError(t, blocks.Add(th.cdb, block))
	}
	data := encodeProposal(t, p)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), toIds(p.Votes.Support)).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(nil).DoAndReturn(
		func(got *types.Ballot) error {
			require.NoError(t, ballots.Add(th.cdb, got))
			return nil
		})
	th.mf.EXPECT().GetProposalTxs(gomock.Any(), p.TxIDs).Return(nil).Times(1)
	th.mf.EXPECT().RegisterPeerHashes(p2p.NoPeer, collectHashes(*p))
	th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(nil).Times(1)
	require.NoError(t, th.HandleSyncedProposal(context.TODO(), data))
	checkProposal(t, th.cdb, p, true)
	counts, err := testutil.GatherAndCount(prometheus.DefaultGatherer, "spacemesh_proposals_proposal_size")
	require.NoError(t, err)
	require.Equal(t, 1, counts)
	counts, err = testutil.GatherAndCount(prometheus.DefaultGatherer, "spacemesh_proposals_num_txs_in_proposal")
	require.NoError(t, err)
	require.Equal(t, 1, counts)
	counts, err = testutil.GatherAndCount(prometheus.DefaultGatherer, "spacemesh_proposals_num_blocks_in_exception")
	require.NoError(t, err)
	require.Equal(t, 3, counts)
}

func TestCollectHashes(t *testing.T) {
	p := createProposal(t)
	b := p.Ballot
	expected := []types.Hash32{b.RefBallot.AsHash32()}
	expected = append(expected, b.Votes.Base.AsHash32())
	expected = append(expected, types.BlockIDsToHashes(ballotBlockView(&b))...)
	require.ElementsMatch(t, expected, collectHashes(b))

	expected = append(expected, types.TransactionIDsToHashes(p.TxIDs)...)
	require.ElementsMatch(t, expected, collectHashes(*p))
}

func toIds(votes []types.Vote) (rst []types.BlockID) {
	for _, vote := range votes {
		rst = append(rst, vote.ID)
	}
	return rst
}
