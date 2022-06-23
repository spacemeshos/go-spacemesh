package proposals

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/proposals/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
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
	}
}

func createTestHandler(t *testing.T) *testHandler {
	types.SetLayersPerEpoch(layersPerEpoch)
	ms := fullMockSet(t)
	return &testHandler{
		Handler: NewHandler(datastore.NewCachedDB(sql.InMemory(), logtest.New(t)), ms.mf, ms.mbc, ms.mm,
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

func createProposal(t *testing.T) *types.Proposal {
	t.Helper()
	b := types.RandomBallot()
	signer := signing.NewEdSigner()
	b.Signature = signer.Sign(b.Bytes())
	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: *b,
			TxIDs:  []types.TransactionID{types.RandomTransactionID(), types.RandomTransactionID()},
		},
	}
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

func createBallot(t *testing.T) *types.Ballot {
	t.Helper()
	return signAndInit(t, types.RandomBallot())
}

func signAndInit(t *testing.T, b *types.Ballot) *types.Ballot {
	t.Helper()
	b.Signature = signing.NewEdSigner().Sign(b.Bytes())
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
	b.Signature = signing.NewEdSigner().Sign(b.Bytes())
	require.NoError(t, b.Initialize())
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

func checkBallot(t *testing.T, cdb *datastore.CachedDB, b *types.Ballot, exist, malicious bool) {
	t.Helper()
	gotB, err := ballots.Get(cdb, b.ID())
	if exist {
		require.NoError(t, err)
		require.Equal(t, b, gotB)
	} else {
		require.ErrorIs(t, err, sql.ErrNotFound)
	}
	gotM, err := identities.IsMalicious(cdb, b.SmesherID().Bytes())
	require.NoError(t, err)
	require.Equal(t, malicious, gotM)
}

func TestBallot_MalformedData(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	data, err := codec.Encode(b.InnerBallot)
	require.NoError(t, err)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errMalformedData)
}

func TestBallot_BadSignature(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	b.Signature = b.Signature[1:]
	data := encodeBallot(t, b)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errInitialize)
}

func TestBallot_KnownBallot(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	require.NoError(t, ballots.Add(th.cdb, b))
	data := encodeBallot(t, b)
	require.NoError(t, th.HandleBallotData(context.TODO(), data))
}

func TestBallot_EmptyATXID(t *testing.T) {
	th := createTestHandler(t)
	b := types.RandomBallot()
	b.AtxID = *types.EmptyATXID
	b = signAndInit(t, b)
	data := encodeBallot(t, b)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errInvalidATXID)
}

func TestBallot_GoldenATXID(t *testing.T) {
	th := createTestHandler(t)
	b := types.RandomBallot()
	b.AtxID = genGoldenATXID()
	b = signAndInit(t, b)
	data := encodeBallot(t, b)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errInvalidATXID)
}

func TestBallot_MissingBaseBallot(t *testing.T) {
	th := createTestHandler(t)
	b := types.RandomBallot()
	b.Votes.Base = types.EmptyBallotID
	b = signAndInit(t, b)
	data := encodeBallot(t, b)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errMissingBaseBallot)
}

func TestBallot_RefBallotMissingEpochData(t *testing.T) {
	th := createTestHandler(t)
	b := createRefBallot(t)
	b.EpochData = nil
	data := encodeBallot(t, b)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errMissingEpochData)
}

func TestBallot_RefBallotMissingBeacon(t *testing.T) {
	th := createTestHandler(t)
	b := createRefBallot(t)
	b.EpochData.Beacon = types.EmptyBeacon
	data := encodeBallot(t, b)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errMissingBeacon)
}

func TestBallot_RefBallotEmptyActiveSet(t *testing.T) {
	th := createTestHandler(t)
	b := createRefBallot(t)
	b.EpochData.ActiveSet = nil
	data := encodeBallot(t, b)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errEmptyActiveSet)
}

func TestBallot_RefBallotDuplicateInActiveSet(t *testing.T) {
	th := createTestHandler(t)
	b := createRefBallot(t)
	b.EpochData.ActiveSet = append(b.EpochData.ActiveSet, b.EpochData.ActiveSet[0])
	data := encodeBallot(t, b)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errDuplicateATX)
}

func TestBallot_NotRefBallotButHasEpochData(t *testing.T) {
	th := createTestHandler(t)
	b := types.RandomBallot()
	b.EpochData = &types.EpochData{}
	b = signAndInit(t, b)
	data := encodeBallot(t, b)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errUnexpectedEpochData)
	checkBallot(t, th.cdb, b, false, false)
}

func TestBallot_BallotDoubleVotedWithinHdist(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	require.GreaterOrEqual(t, 2, len(b.Votes.Support))
	cutoff := b.LayerIndex.Sub(th.cfg.Hdist)
	for _, bid := range b.Votes.Support {
		blk := types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: cutoff.Add(1)})
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(nil).Times(1)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errDoubleVoting)
	checkBallot(t, th.cdb, b, false, true)
}

func TestBallot_BallotDoubleVotedWithinHdist_LyrBfrHdist(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	th.cfg.Hdist = b.LayerIndex.Add(1).Uint32()
	require.GreaterOrEqual(t, 2, len(b.Votes.Support))
	for _, bid := range b.Votes.Support {
		blk := types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: b.LayerIndex.Sub(1)})
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(nil).Times(1)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errDoubleVoting)
	checkBallot(t, th.cdb, b, false, true)
}

func TestBallot_BallotDoubleVotedOutsideHdist(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	cutoff := b.LayerIndex.Sub(th.cfg.Hdist)
	for _, bid := range b.Votes.Support {
		blk := types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: cutoff.Sub(1)})
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return true, nil
		})
	require.NoError(t, th.HandleBallotData(context.TODO(), data))
	got, err := ballots.Get(th.cdb, b.ID())
	require.NoError(t, err)
	require.Equal(t, b, got)
}

func TestBallot_ConflictingForAndAgainst(t *testing.T) {
	th := createTestHandler(t)
	b := types.RandomBallot()
	b.Votes.Against = b.Votes.Support
	b = signAndInit(t, b)
	for i, bid := range b.Votes.Support {
		blk := types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: b.LayerIndex.Sub(uint32(i + 1))})
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), append(b.Votes.Support, b.Votes.Against...)).Return(nil).Times(1)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errConflictingExceptions)
	checkBallot(t, th.cdb, b, false, true)
}

func TestBallot_ConflictingForAndAbstain(t *testing.T) {
	th := createTestHandler(t)
	b := types.RandomBallot()
	b.Votes.Abstain = []types.LayerID{b.LayerIndex.Sub(1)}
	b = signAndInit(t, b)
	for i, bid := range b.Votes.Support {
		blk := types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: b.LayerIndex.Sub(uint32(i + 1))})
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(nil).Times(1)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errConflictingExceptions)
	checkBallot(t, th.cdb, b, false, true)
}

func TestBallot_ConflictingAgainstAndAbstain(t *testing.T) {
	th := createTestHandler(t)
	b := types.RandomBallot()
	b.Votes.Against = b.Votes.Support
	b.Votes.Support = nil
	b.Votes.Abstain = []types.LayerID{b.LayerIndex.Sub(1)}
	b = signAndInit(t, b)
	for i, bid := range b.Votes.Against {
		blk := types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: b.LayerIndex.Sub(uint32(i + 1))})
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), append(b.Votes.Support, b.Votes.Against...)).Return(nil).Times(1)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errConflictingExceptions)
	checkBallot(t, th.cdb, b, false, true)
}

func TestBallot_ExceedMaxExceptions(t *testing.T) {
	th := createTestHandler(t)
	b := types.RandomBallot()
	b.Votes.Support = append(b.Votes.Support, types.RandomBlockID(), types.RandomBlockID())
	b = signAndInit(t, b)
	for i, bid := range b.Votes.Support {
		blk := types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: b.LayerIndex.Sub(uint32(i + 1))})
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(nil).Times(1)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errExceptionsOverflow)
	checkBallot(t, th.cdb, b, false, false)
}

func TestBallot_BallotsNotAvailable(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	data := encodeBallot(t, b)
	errUnknown := errors.New("unknown")
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(errUnknown).Times(1)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errUnknown)
	checkBallot(t, th.cdb, b, false, false)
}

func TestBallot_ATXsNotAvailable(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	data := encodeBallot(t, b)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	errUnknown := errors.New("unknown")
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(errUnknown).Times(1)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errUnknown)
	checkBallot(t, th.cdb, b, false, false)
}

func TestBallot_BlocksNotAvailable(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	data := encodeBallot(t, b)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	errUnknown := errors.New("unknown")
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(errUnknown).Times(1)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errUnknown)
	checkBallot(t, th.cdb, b, false, false)
}

func TestBallot_ErrorCheckingEligible(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	for i, bid := range b.Votes.Support {
		blk := types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: b.LayerIndex.Sub(uint32(i + 1))})
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return false, errors.New("unknown")
		})
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errNotEligible)
	checkBallot(t, th.cdb, b, false, false)
}

func TestBallot_NotEligible(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	for i, bid := range b.Votes.Support {
		blk := types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: b.LayerIndex.Sub(uint32(i + 1))})
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return false, nil
		})
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data), errNotEligible)
	checkBallot(t, th.cdb, b, false, false)
}

func TestBallot_Success(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	for i, bid := range b.Votes.Support {
		blk := types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: b.LayerIndex.Sub(uint32(i + 1))})
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return true, nil
		})
	require.NoError(t, th.HandleBallotData(context.TODO(), data))
	checkBallot(t, th.cdb, b, true, false)
}

func TestBallot_RefBallot(t *testing.T) {
	th := createTestHandler(t)
	b := createRefBallot(t)
	for i, bid := range b.Votes.Support {
		blk := types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: b.LayerIndex.Sub(uint32(i + 1))})
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeBallot(t, b)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base}).Return(nil).Times(1)
	atxIDs := types.ATXIDList{b.AtxID}
	atxIDs = append(atxIDs, b.EpochData.ActiveSet...)
	th.mf.EXPECT().GetAtxs(gomock.Any(), atxIDs).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return true, nil
		})
	require.NoError(t, th.HandleBallotData(context.TODO(), data))
	checkBallot(t, th.cdb, b, true, false)
}

func TestProposal_MalformedData(t *testing.T) {
	th := createTestHandler(t)
	p := createProposal(t)
	data, err := codec.Encode(p.InnerProposal)
	require.NoError(t, err)
	require.ErrorIs(t, th.HandleProposalData(context.TODO(), data), errMalformedData)
	checkProposal(t, th.cdb, p, false)
}

func TestProposal_BadSignature(t *testing.T) {
	th := createTestHandler(t)
	p := createProposal(t)
	p.Signature = p.Signature[1:]
	data := encodeProposal(t, p)
	require.ErrorIs(t, th.HandleProposalData(context.TODO(), data), errInitialize)
	checkProposal(t, th.cdb, p, false)
}

func TestProposal_KnownProposal(t *testing.T) {
	th := createTestHandler(t)
	p := createProposal(t)
	require.NoError(t, ballots.Add(th.cdb, &p.Ballot))
	require.NoError(t, proposals.Add(th.cdb, p))
	data := encodeProposal(t, p)
	require.NoError(t, th.HandleProposalData(context.TODO(), data))
	require.Equal(t, pubsub.ValidationIgnore, th.HandleProposal(context.TODO(), "", data))
	checkProposal(t, th.cdb, p, true)
}

func TestProposal_DuplicateTXs(t *testing.T) {
	th := createTestHandler(t)
	b := types.RandomBallot()
	signer := signing.NewEdSigner()
	b.Signature = signer.Sign(b.Bytes())
	tid := types.RandomTransactionID()
	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: *b,
			TxIDs:  []types.TransactionID{tid, tid},
		},
	}
	p.Signature = signer.Sign(p.Bytes())
	require.NoError(t, p.Initialize())
	for i, bid := range p.Votes.Support {
		blk := types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: p.LayerIndex.Sub(uint32(i + 1))})
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeProposal(t, p)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), p.Votes.Support).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	require.ErrorIs(t, th.HandleProposalData(context.TODO(), data), errDuplicateTX)
	checkProposal(t, th.cdb, p, false)
}

func TestProposal_TXsNotAvailable(t *testing.T) {
	th := createTestHandler(t)
	p := createProposal(t)
	for i, bid := range p.Votes.Support {
		blk := types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: p.LayerIndex.Sub(uint32(i + 1))})
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeProposal(t, p)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), p.Votes.Support).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	errUnknown := errors.New("unknown")
	th.mf.EXPECT().GetProposalTxs(gomock.Any(), p.TxIDs).Return(errUnknown).Times(1)
	require.ErrorIs(t, th.HandleProposalData(context.TODO(), data), errUnknown)
	checkProposal(t, th.cdb, p, false)
}

func TestProposal_FailedToAddProposalTXs(t *testing.T) {
	th := createTestHandler(t)
	p := createProposal(t)
	for i, bid := range p.Votes.Support {
		blk := types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: p.LayerIndex.Sub(uint32(i + 1))})
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeProposal(t, p)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), p.Votes.Support).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mf.EXPECT().GetProposalTxs(gomock.Any(), p.TxIDs).Return(nil).Times(1)
	errUnknown := errors.New("unknown")
	th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(errUnknown).Times(1)
	require.ErrorIs(t, th.HandleProposalData(context.TODO(), data), errUnknown)
	checkProposal(t, th.cdb, p, true)
}

func TestProposal_ValidProposal(t *testing.T) {
	th := createTestHandler(t)
	p := createProposal(t)
	for i, bid := range p.Votes.Support {
		blk := types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: p.LayerIndex.Sub(uint32(i + 1))})
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeProposal(t, p)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), p.Votes.Support).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mf.EXPECT().GetProposalTxs(gomock.Any(), p.TxIDs).Return(nil).Times(1)
	th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(nil).Times(1)
	require.NoError(t, th.HandleProposalData(context.TODO(), data))
	checkProposal(t, th.cdb, p, true)
}

func TestMetrics(t *testing.T) {
	th := createTestHandler(t)
	p := createProposal(t)
	for i, bid := range p.Votes.Support {
		blk := types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: p.LayerIndex.Sub(uint32(i + 1))})
		require.NoError(t, blocks.Add(th.cdb, blk))
	}
	data := encodeProposal(t, p)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}).Return(nil).Times(1)
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), p.Votes.Support).Return(nil).Times(1)
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mf.EXPECT().GetProposalTxs(gomock.Any(), p.TxIDs).Return(nil).Times(1)
	th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(nil).Times(1)
	require.NoError(t, th.HandleProposalData(context.TODO(), data))
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
