package proposals

import (
	"context"
	"errors"
	"testing"

	"github.com/spacemeshos/go-spacemesh/p2p"

	"github.com/golang/mock/gomock"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/proposals/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
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
	ctrl *gomock.Controller
	mf   *smocks.MockFetcher
	mbc  *smocks.MockBeaconCollector
	mdb  *mocks.MockatxDB
	mm   *mocks.MockmeshDB
	mp   *mocks.MockproposalDB
	mv   *mocks.MockeligibilityValidator
}

type testHandler struct {
	*Handler
	*mockSet
}

func fullMockSet(tb testing.TB) *mockSet {
	ctrl := gomock.NewController(tb)
	return &mockSet{
		ctrl: ctrl,
		mf:   smocks.NewMockFetcher(ctrl),
		mbc:  smocks.NewMockBeaconCollector(ctrl),
		mdb:  mocks.NewMockatxDB(ctrl),
		mm:   mocks.NewMockmeshDB(ctrl),
		mp:   mocks.NewMockproposalDB(ctrl),
		mv:   mocks.NewMockeligibilityValidator(ctrl),
	}
}

func createTestHandler(tb testing.TB) *testHandler {
	types.SetLayersPerEpoch(layersPerEpoch)
	ms := fullMockSet(tb)
	return &testHandler{
		Handler: NewHandler(ms.mf, ms.mbc, ms.mdb, ms.mm, ms.mp,
			WithGoldenATXID(genGoldenATXID()),
			WithMaxExceptions(3),
			WithLayerPerEpoch(layersPerEpoch),
			WithLayerSize(layerAvgSize),
			withValidator(ms.mv)),
		mockSet: ms,
	}
}

func createProposal(tb testing.TB) *types.Proposal {
	tb.Helper()
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
	require.NoError(tb, p.Initialize())
	return p
}

func encodeProposal(tb testing.TB, p *types.Proposal) []byte {
	tb.Helper()
	data, err := codec.Encode(p)
	require.NoError(tb, err)
	return data
}

func createBallot(tb testing.TB) *types.Ballot {
	tb.Helper()
	b := types.RandomBallot()
	b.Signature = signing.NewEdSigner().Sign(b.Bytes())
	require.NoError(tb, b.Initialize())
	return b
}

func createRefBallot(tb testing.TB) *types.Ballot {
	tb.Helper()
	b := types.RandomBallot()
	b.RefBallot = types.EmptyBallotID
	b.EpochData = &types.EpochData{
		ActiveSet: types.ATXIDList{types.RandomATXID(), types.RandomATXID()},
		Beacon:    types.RandomBeacon(),
	}
	b.Signature = signing.NewEdSigner().Sign(b.Bytes())
	require.NoError(tb, b.Initialize())
	return b
}

func encodeBallot(tb testing.TB, b *types.Ballot) []byte {
	tb.Helper()
	data, err := codec.Encode(b)
	require.NoError(tb, err)
	return data
}

func TestBallot_MalformedData(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createBallot(t)
	data, err := codec.Encode(b.InnerBallot)
	require.NoError(t, err)
	assert.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errMalformedData)
}

func TestBallot_BadSignature(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createBallot(t)
	b.Signature = b.Signature[1:]
	data := encodeBallot(t, b)
	assert.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errInitialize)
}

func TestBallot_KnownBallot(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createBallot(t)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(gomock.Any()).Return(true).Times(1)
	assert.NoError(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer))
}

func TestBallot_EmptyATXID(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createBallot(t)
	b.AtxID = *types.EmptyATXID
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(gomock.Any()).Return(false).Times(1)
	assert.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errInvalidATXID)
}

func TestBallot_GoldenATXID(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createBallot(t)
	b.AtxID = genGoldenATXID()
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(gomock.Any()).Return(false).Times(1)
	assert.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errInvalidATXID)
}

func TestBallot_MissingBaseBallot(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createBallot(t)
	b.Votes.Base = types.EmptyBallotID
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(gomock.Any()).Return(false).Times(1)
	assert.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errMissingBaseBallot)
}

func TestBallot_RefBallotMissingEpochData(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createRefBallot(t)
	b.EpochData = nil
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(gomock.Any()).Return(false).Times(1)
	assert.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errMissingEpochData)
}

func TestBallot_RefBallotMissingBeacon(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createRefBallot(t)
	b.EpochData.Beacon = types.EmptyBeacon
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(gomock.Any()).Return(false).Times(1)
	assert.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errMissingBeacon)
}

func TestBallot_RefBallotEmptyActiveSet(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createRefBallot(t)
	b.EpochData.ActiveSet = nil
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(gomock.Any()).Return(false).Times(1)
	assert.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errEmptyActiveSet)
}

func TestBallot_RefBallotDuplicateInActiveSet(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createRefBallot(t)
	b.EpochData.ActiveSet = append(b.EpochData.ActiveSet, b.EpochData.ActiveSet[0])
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(gomock.Any()).Return(false).Times(1)
	assert.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errDuplicateATX)
}

func TestBallot_NotRefBallotButHasEpochData(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createBallot(t)
	b.EpochData = &types.EpochData{}
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(gomock.Any()).Return(false).Times(1)
	assert.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errUnexpectedEpochData)
}

func TestBallot_ConflictingExceptions(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createBallot(t)
	b.Votes.Against = b.Votes.Support
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(gomock.Any()).Return(false).Times(1)
	assert.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errConflictingExceptions)
}

func TestBallot_ExceedMaxExceptions(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createBallot(t)
	b.Votes.Support = append(b.Votes.Support, types.RandomBlockID(), types.RandomBlockID())
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(gomock.Any()).Return(false).Times(1)
	assert.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errExceptionsOverflow)
}

func TestBallot_BallotsNotAvailable(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createBallot(t)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	errUnknown := errors.New("unknown")
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}, p2p.AnyPeer).Return(errUnknown).Times(1)
	th.mf.EXPECT().TrackBallotPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	assert.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errUnknown)
}

func TestBallot_ATXsNotAvailable(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createBallot(t)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	errUnknown := errors.New("unknown")
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(errUnknown).Times(1)
	assert.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errUnknown)
}

func TestBallot_BlocksNotAvailable(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createBallot(t)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	errUnknown := errors.New("unknown")
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(errUnknown).Times(1)
	th.mf.EXPECT().TrackBlockPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	assert.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errUnknown)
}

func TestBallot_ErrorCheckingEligible(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createBallot(t)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlockPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			assert.Equal(t, b.ID(), ballot.ID())
			return false, errors.New("unknown")
		})
	assert.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errNotEligible)
}

func TestBallot_NotEligible(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createBallot(t)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlockPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			assert.Equal(t, b.ID(), ballot.ID())
			return false, nil
		})
	assert.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errNotEligible)
}

func TestBallot_Success(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createBallot(t)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlockPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ballot *types.Ballot) (bool, error) {
			assert.Equal(t, b.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(gomock.Any()).DoAndReturn(
		func(ballot *types.Ballot) error {
			assert.Equal(t, b.ID(), ballot.ID())
			return nil
		}).Times(1)
	assert.NoError(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer))
}

func TestBallot_RefBallot(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := createRefBallot(t)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	atxIDs := types.ATXIDList{b.AtxID}
	atxIDs = append(atxIDs, b.EpochData.ActiveSet...)
	th.mf.EXPECT().GetAtxs(gomock.Any(), atxIDs).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlockPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ballot *types.Ballot) (bool, error) {
			assert.Equal(t, b.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(gomock.Any()).DoAndReturn(
		func(ballot *types.Ballot) error {
			assert.Equal(t, b.ID(), ballot.ID())
			return nil
		}).Times(1)
	assert.NoError(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer))
}

func TestProposal_MalformedData(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	p := createProposal(t)
	data, err := codec.Encode(p.InnerProposal)
	require.NoError(t, err)
	assert.ErrorIs(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer), errMalformedData)
}

func TestProposal_BadSignature(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	p := createProposal(t)
	p.Signature = p.Signature[1:]
	data := encodeProposal(t, p)
	assert.ErrorIs(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer), errInitialize)
}

func TestProposal_KnownProposal(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	p := createProposal(t)
	data := encodeProposal(t, p)
	th.mp.EXPECT().HasProposal(p.ID()).Return(true).Times(2)
	assert.NoError(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer))
	assert.Equal(t, pubsub.ValidationIgnore, th.HandleProposal(context.TODO(), "", data))
}

func TestProposal_FailedToAddBallot(t *testing.T) {
	th := createTestHandler(t)
	p := createProposal(t)
	data := encodeProposal(t, p)
	th.mp.EXPECT().HasProposal(p.ID()).Return(false).Times(1)
	th.mm.EXPECT().HasBallot(p.Ballot.ID()).Return(false).Times(1)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), p.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlockPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			assert.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	errUnknown := errors.New("unknown")
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(errUnknown).Times(1)
	assert.ErrorIs(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer), errUnknown)
}

func TestProposal_DuplicateTXs(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	b := types.RandomBallot()
	signer := signing.NewEdSigner()
	b.Signature = signer.Sign(b.Bytes())
	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: *b,
			TxIDs:  []types.TransactionID{types.RandomTransactionID(), types.RandomTransactionID()},
		},
	}
	p.TxIDs = append(p.TxIDs, p.TxIDs[0])
	p.Signature = signer.Sign(p.Bytes())
	require.NoError(t, p.Initialize())
	data := encodeProposal(t, p)
	th.mp.EXPECT().HasProposal(p.ID()).Return(false).Times(1)
	th.mm.EXPECT().HasBallot(p.Ballot.ID()).Return(false).Times(1)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), p.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlockPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			assert.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(nil).Times(1)
	assert.ErrorIs(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer), errDuplicateTX)
}

func TestProposal_TXsNotAvailable(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	p := createProposal(t)
	data := encodeProposal(t, p)
	th.mp.EXPECT().HasProposal(p.ID()).Return(false).Times(1)
	th.mm.EXPECT().HasBallot(p.Ballot.ID()).Return(false).Times(1)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), p.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlockPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			assert.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(nil).Times(1)
	errUnknown := errors.New("unknown")
	th.mf.EXPECT().GetTxs(gomock.Any(), p.TxIDs).Return(errUnknown).Times(1)
	assert.ErrorIs(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer), errUnknown)
}

func TestProposal_FailedToAddProposal(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	p := createProposal(t)
	data := encodeProposal(t, p)
	th.mp.EXPECT().HasProposal(p.ID()).Return(false).Times(1)
	th.mm.EXPECT().HasBallot(p.Ballot.ID()).Return(false).Times(1)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), p.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlockPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			assert.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(nil).Times(1)
	th.mf.EXPECT().GetTxs(gomock.Any(), p.TxIDs).Return(nil).Times(1)
	errUnknown := errors.New("unknown")
	th.mp.EXPECT().AddProposal(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, proposal *types.Proposal) error {
			assert.Equal(t, p, proposal)
			return errUnknown
		}).Times(1)
	assert.ErrorIs(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer), errUnknown)
}

func TestProposal_FailedToAddProposalTXs(t *testing.T) {
	th := createTestHandler(t)
	p := createProposal(t)
	data := encodeProposal(t, p)
	th.mp.EXPECT().HasProposal(p.ID()).Return(false).Times(1)
	th.mm.EXPECT().HasBallot(p.Ballot.ID()).Return(false).Times(1)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), p.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlockPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			assert.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(nil).Times(1)
	th.mf.EXPECT().GetTxs(gomock.Any(), p.TxIDs).Return(nil).Times(1)
	th.mp.EXPECT().AddProposal(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, proposal *types.Proposal) error {
			assert.Equal(t, p, proposal)
			return nil
		}).Times(1)
	errUnknown := errors.New("unknown")
	th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(errUnknown).Times(1)
	assert.ErrorIs(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer), errUnknown)
}

func TestProposal_ValidProposal(t *testing.T) {
	th := createTestHandler(t)
	p := createProposal(t)
	data := encodeProposal(t, p)
	th.mp.EXPECT().HasProposal(p.ID()).Return(false).Times(1)
	th.mm.EXPECT().HasBallot(p.Ballot.ID()).Return(false).Times(1)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), p.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlockPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			assert.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(nil).Times(1)
	th.mf.EXPECT().GetTxs(gomock.Any(), p.TxIDs).Return(nil).Times(1)
	th.mp.EXPECT().AddProposal(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, proposal *types.Proposal) error {
			assert.Equal(t, p, proposal)
			return nil
		}).Times(1)
	th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(nil).Times(1)
	assert.NoError(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer))
}

func TestMetrics(t *testing.T) {
	th := createTestHandler(t)
	defer th.ctrl.Finish()
	p := createProposal(t)
	data := encodeProposal(t, p)
	th.mp.EXPECT().HasProposal(p.ID()).Return(false).Times(1)
	th.mm.EXPECT().HasBallot(p.Ballot.ID()).Return(false).Times(1)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), p.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlockPeers(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			assert.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(nil).Times(1)
	th.mf.EXPECT().GetTxs(gomock.Any(), p.TxIDs).Return(nil).Times(1)
	th.mp.EXPECT().AddProposal(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, proposal *types.Proposal) error {
			assert.Equal(t, p, proposal)
			return nil
		}).Times(1)
	th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(nil).Times(1)
	assert.NoError(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer))
	counts, err := testutil.GatherAndCount(prometheus.DefaultGatherer, "spacemesh_proposals_proposal_size")
	require.NoError(t, err)
	assert.Equal(t, 1, counts)
	counts, err = testutil.GatherAndCount(prometheus.DefaultGatherer, "spacemesh_proposals_num_txs_in_proposal")
	require.NoError(t, err)
	assert.Equal(t, 1, counts)
	counts, err = testutil.GatherAndCount(prometheus.DefaultGatherer, "spacemesh_proposals_num_blocks_in_exception")
	require.NoError(t, err)
	assert.Equal(t, 3, counts)
}
