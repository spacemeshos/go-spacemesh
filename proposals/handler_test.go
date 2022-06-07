package proposals

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/proposals/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
	"github.com/stretchr/testify/require"
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
	mdb *mocks.MockatxDB
	mm  *mocks.MockmeshDB
	mp  *mocks.MockproposalDB
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
		mdb: mocks.NewMockatxDB(ctrl),
		mm:  mocks.NewMockmeshDB(ctrl),
		mp:  mocks.NewMockproposalDB(ctrl),
		mv:  mocks.NewMockeligibilityValidator(ctrl),
	}
}

func createTestHandler(t *testing.T) *testHandler {
	types.SetLayersPerEpoch(layersPerEpoch)
	ms := fullMockSet(t)
	return &testHandler{
		Handler: NewHandler(ms.mf, ms.mbc, ms.mdb, ms.mm, ms.mp,
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

func TestBallot_MalformedData(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	data, err := codec.Encode(b.InnerBallot)
	require.NoError(t, err)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errMalformedData)
}

func TestBallot_BadSignature(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	b.Signature = b.Signature[1:]
	data := encodeBallot(t, b)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errInitialize)
}

func TestBallot_KnownBallot(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(true).Times(1)
	require.NoError(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer))
}

func TestBallot_EmptyATXID(t *testing.T) {
	th := createTestHandler(t)
	b := types.RandomBallot()
	b.AtxID = *types.EmptyATXID
	b = signAndInit(t, b)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errInvalidATXID)
}

func TestBallot_GoldenATXID(t *testing.T) {
	th := createTestHandler(t)
	b := types.RandomBallot()
	b.AtxID = genGoldenATXID()
	b = signAndInit(t, b)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errInvalidATXID)
}

func TestBallot_MissingBaseBallot(t *testing.T) {
	th := createTestHandler(t)
	b := types.RandomBallot()
	b.Votes.Base = types.EmptyBallotID
	b = signAndInit(t, b)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errMissingBaseBallot)
}

func TestBallot_RefBallotMissingEpochData(t *testing.T) {
	th := createTestHandler(t)
	b := createRefBallot(t)
	b.EpochData = nil
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(gomock.Any()).Return(false).Times(1)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errMissingEpochData)
}

func TestBallot_RefBallotMissingBeacon(t *testing.T) {
	th := createTestHandler(t)
	b := createRefBallot(t)
	b.EpochData.Beacon = types.EmptyBeacon
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(gomock.Any()).Return(false).Times(1)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errMissingBeacon)
}

func TestBallot_RefBallotEmptyActiveSet(t *testing.T) {
	th := createTestHandler(t)
	b := createRefBallot(t)
	b.EpochData.ActiveSet = nil
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(gomock.Any()).Return(false).Times(1)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errEmptyActiveSet)
}

func TestBallot_RefBallotDuplicateInActiveSet(t *testing.T) {
	th := createTestHandler(t)
	b := createRefBallot(t)
	b.EpochData.ActiveSet = append(b.EpochData.ActiveSet, b.EpochData.ActiveSet[0])
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(gomock.Any()).Return(false).Times(1)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errDuplicateATX)
}

func TestBallot_NotRefBallotButHasEpochData(t *testing.T) {
	th := createTestHandler(t)
	b := types.RandomBallot()
	b.EpochData = &types.EpochData{}
	b = signAndInit(t, b)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errUnexpectedEpochData)
}

func TestBallot_BallotDoubleVotedWithinHdist(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	require.GreaterOrEqual(t, 2, len(b.Votes.Support))
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	cutoff := b.LayerIndex.Sub(th.cfg.Hdist)
	th.mm.EXPECT().GetBlockLayer(b.Votes.Support[0]).Return(cutoff.Add(1), nil)
	th.mm.EXPECT().GetBlockLayer(b.Votes.Support[1]).Return(cutoff.Add(1), nil)
	th.mm.EXPECT().SetIdentityMalicious(b.SmesherID()).Return(nil)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errDoubleVoting)
}

func TestBallot_BallotDoubleVotedWithinHdist_LyrBfrHdist(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	th.cfg.Hdist = b.LayerIndex.Add(1).Uint32()
	require.GreaterOrEqual(t, 2, len(b.Votes.Support))
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	th.mm.EXPECT().GetBlockLayer(b.Votes.Support[0]).Return(b.LayerIndex.Sub(1), nil)
	th.mm.EXPECT().GetBlockLayer(b.Votes.Support[1]).Return(b.LayerIndex.Sub(1), nil)
	th.mm.EXPECT().SetIdentityMalicious(b.SmesherID()).Return(nil)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errDoubleVoting)
}

func TestBallot_BallotDoubleVotedWithinHdist_SetMaliciousError(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	require.GreaterOrEqual(t, 2, len(b.Votes.Support))
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	cutoff := b.LayerIndex.Sub(th.cfg.Hdist)
	th.mm.EXPECT().GetBlockLayer(b.Votes.Support[0]).Return(cutoff, nil)
	th.mm.EXPECT().GetBlockLayer(b.Votes.Support[1]).Return(cutoff, nil)
	errUnknown := errors.New("unknown")
	th.mm.EXPECT().SetIdentityMalicious(b.SmesherID()).Return(errUnknown)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errUnknown)
}

func TestBallot_BallotDoubleVotedOutsideHdist(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	cutoff := b.LayerIndex.Sub(th.cfg.Hdist)
	for _, bid := range b.Votes.Support {
		th.mm.EXPECT().GetBlockLayer(bid).Return(cutoff.Sub(1), nil)
	}
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotsPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlocksPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(gomock.Any()).DoAndReturn(
		func(ballot *types.Ballot) error {
			require.Equal(t, b.ID(), ballot.ID())
			return nil
		}).Times(1)
	require.NoError(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer))
}

func TestBallot_ConflictingForAndAgainst(t *testing.T) {
	th := createTestHandler(t)
	b := types.RandomBallot()
	b.Votes.Against = b.Votes.Support
	b = signAndInit(t, b)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	for i, bid := range b.Votes.Support {
		th.mm.EXPECT().GetBlockLayer(bid).Return(b.LayerIndex.Sub(uint32(i+1)), nil)
	}
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errConflictingExceptions)
}

func TestBallot_ConflictingForAndAbstain(t *testing.T) {
	th := createTestHandler(t)
	b := types.RandomBallot()
	b.Votes.Abstain = []types.LayerID{b.LayerIndex.Sub(1)}
	b = signAndInit(t, b)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	for i, bid := range b.Votes.Support {
		th.mm.EXPECT().GetBlockLayer(bid).Return(b.LayerIndex.Sub(uint32(i+1)), nil)
	}
	th.mm.EXPECT().SetIdentityMalicious(b.SmesherID()).Return(nil)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errConflictingExceptions)
}

func TestBallot_ConflictingAgainstAndAbstain(t *testing.T) {
	th := createTestHandler(t)
	b := types.RandomBallot()
	b.Votes.Against = b.Votes.Support
	b.Votes.Support = nil
	b.Votes.Abstain = []types.LayerID{b.LayerIndex.Sub(1)}
	b = signAndInit(t, b)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	for i, bid := range b.Votes.Against {
		th.mm.EXPECT().GetBlockLayer(bid).Return(b.LayerIndex.Sub(uint32(i+1)), nil)
	}
	th.mm.EXPECT().SetIdentityMalicious(b.SmesherID()).Return(nil)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errConflictingExceptions)
}

func TestBallot_ExceedMaxExceptions(t *testing.T) {
	th := createTestHandler(t)
	b := types.RandomBallot()
	b.Votes.Support = append(b.Votes.Support, types.RandomBlockID(), types.RandomBlockID())
	b = signAndInit(t, b)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	for i, bid := range b.Votes.Support {
		th.mm.EXPECT().GetBlockLayer(bid).Return(b.LayerIndex.Sub(uint32(i+1)), nil)
	}
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errExceptionsOverflow)
}

func TestBallot_BallotsNotAvailable(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	for i, bid := range b.Votes.Support {
		th.mm.EXPECT().GetBlockLayer(bid).Return(b.LayerIndex.Sub(uint32(i+1)), nil)
	}
	errUnknown := errors.New("unknown")
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}, p2p.AnyPeer).Return(errUnknown).Times(1)
	th.mf.EXPECT().TrackBallotsPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errUnknown)
}

func TestBallot_ATXsNotAvailable(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	for i, bid := range b.Votes.Support {
		th.mm.EXPECT().GetBlockLayer(bid).Return(b.LayerIndex.Sub(uint32(i+1)), nil)
	}
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotsPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	errUnknown := errors.New("unknown")
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(errUnknown).Times(1)
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errUnknown)
}

func TestBallot_BlocksNotAvailable(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	for i, bid := range b.Votes.Support {
		th.mm.EXPECT().GetBlockLayer(bid).Return(b.LayerIndex.Sub(uint32(i+1)), nil)
	}
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotsPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	errUnknown := errors.New("unknown")
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(errUnknown).Times(1)
	th.mf.EXPECT().TrackBlocksPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errUnknown)
}

func TestBallot_ErrorCheckingEligible(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	for i, bid := range b.Votes.Support {
		th.mm.EXPECT().GetBlockLayer(bid).Return(b.LayerIndex.Sub(uint32(i+1)), nil)
	}
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotsPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlocksPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return false, errors.New("unknown")
		})
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errNotEligible)
}

func TestBallot_NotEligible(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	for i, bid := range b.Votes.Support {
		th.mm.EXPECT().GetBlockLayer(bid).Return(b.LayerIndex.Sub(uint32(i+1)), nil)
	}
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotsPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlocksPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return false, nil
		})
	require.ErrorIs(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer), errNotEligible)
}

func TestBallot_Success(t *testing.T) {
	th := createTestHandler(t)
	b := createBallot(t)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	for i, bid := range b.Votes.Support {
		th.mm.EXPECT().GetBlockLayer(bid).Return(b.LayerIndex.Sub(uint32(i+1)), nil)
	}
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base, b.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotsPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{b.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlocksPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(gomock.Any()).DoAndReturn(
		func(ballot *types.Ballot) error {
			require.Equal(t, b.ID(), ballot.ID())
			return nil
		}).Times(1)
	require.NoError(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer))
}

func TestBallot_RefBallot(t *testing.T) {
	th := createTestHandler(t)
	b := createRefBallot(t)
	data := encodeBallot(t, b)
	th.mm.EXPECT().HasBallot(b.ID()).Return(false).Times(1)
	for i, bid := range b.Votes.Support {
		th.mm.EXPECT().GetBlockLayer(bid).Return(b.LayerIndex.Sub(uint32(i+1)), nil)
	}
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{b.Votes.Base}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotsPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	atxIDs := types.ATXIDList{b.AtxID}
	atxIDs = append(atxIDs, b.EpochData.ActiveSet...)
	th.mf.EXPECT().GetAtxs(gomock.Any(), atxIDs).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), b.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlocksPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, b.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(gomock.Any()).DoAndReturn(
		func(ballot *types.Ballot) error {
			require.Equal(t, b.ID(), ballot.ID())
			return nil
		}).Times(1)
	require.NoError(t, th.HandleBallotData(context.TODO(), data, p2p.AnyPeer))
}

func TestProposal_MalformedData(t *testing.T) {
	th := createTestHandler(t)
	p := createProposal(t)
	data, err := codec.Encode(p.InnerProposal)
	require.NoError(t, err)
	require.ErrorIs(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer), errMalformedData)
}

func TestProposal_BadSignature(t *testing.T) {
	th := createTestHandler(t)
	p := createProposal(t)
	p.Signature = p.Signature[1:]
	data := encodeProposal(t, p)
	require.ErrorIs(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer), errInitialize)
}

func TestProposal_KnownProposal(t *testing.T) {
	th := createTestHandler(t)
	p := createProposal(t)
	data := encodeProposal(t, p)
	th.mp.EXPECT().HasProposal(p.ID()).Return(true).Times(2)
	require.NoError(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer))
	require.Equal(t, pubsub.ValidationIgnore, th.HandleProposal(context.TODO(), "", data))
}

func TestProposal_FailedToAddBallot(t *testing.T) {
	th := createTestHandler(t)
	p := createProposal(t)
	data := encodeProposal(t, p)
	th.mp.EXPECT().HasProposal(p.ID()).Return(false).Times(1)
	th.mm.EXPECT().HasBallot(p.Ballot.ID()).Return(false).Times(1)
	for i, bid := range p.Votes.Support {
		th.mm.EXPECT().GetBlockLayer(bid).Return(p.LayerIndex.Sub(uint32(i+1)), nil)
	}
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotsPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), p.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlocksPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	errUnknown := errors.New("unknown")
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(errUnknown).Times(1)
	require.ErrorIs(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer), errUnknown)
}

func TestProposal_DuplicateTXs(t *testing.T) {
	th := createTestHandler(t)
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
	for i, bid := range b.Votes.Support {
		th.mm.EXPECT().GetBlockLayer(bid).Return(b.LayerIndex.Sub(uint32(i+1)), nil)
	}
	th.mm.EXPECT().HasBallot(p.Ballot.ID()).Return(false).Times(1)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotsPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), p.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlocksPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(nil).Times(1)
	require.ErrorIs(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer), errDuplicateTX)
}

func TestProposal_TXsNotAvailable(t *testing.T) {
	th := createTestHandler(t)
	p := createProposal(t)
	data := encodeProposal(t, p)
	th.mp.EXPECT().HasProposal(p.ID()).Return(false).Times(1)
	for i, bid := range p.Votes.Support {
		th.mm.EXPECT().GetBlockLayer(bid).Return(p.LayerIndex.Sub(uint32(i+1)), nil)
	}
	th.mm.EXPECT().HasBallot(p.Ballot.ID()).Return(false).Times(1)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotsPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), p.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlocksPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(nil).Times(1)
	errUnknown := errors.New("unknown")
	th.mf.EXPECT().GetTxs(gomock.Any(), p.TxIDs).Return(errUnknown).Times(1)
	require.ErrorIs(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer), errUnknown)
}

func TestProposal_FailedToAddProposal(t *testing.T) {
	th := createTestHandler(t)
	p := createProposal(t)
	data := encodeProposal(t, p)
	th.mp.EXPECT().HasProposal(p.ID()).Return(false).Times(1)
	th.mm.EXPECT().HasBallot(p.Ballot.ID()).Return(false).Times(1)
	for i, bid := range p.Votes.Support {
		th.mm.EXPECT().GetBlockLayer(bid).Return(p.LayerIndex.Sub(uint32(i+1)), nil)
	}
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotsPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), p.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlocksPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(nil).Times(1)
	th.mf.EXPECT().GetTxs(gomock.Any(), p.TxIDs).Return(nil).Times(1)
	errUnknown := errors.New("unknown")
	th.mp.EXPECT().AddProposal(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, proposal *types.Proposal) error {
			require.Equal(t, p, proposal)
			return errUnknown
		}).Times(1)
	require.ErrorIs(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer), errUnknown)
}

func TestProposal_FailedToAddProposalTXs(t *testing.T) {
	th := createTestHandler(t)
	p := createProposal(t)
	data := encodeProposal(t, p)
	th.mp.EXPECT().HasProposal(p.ID()).Return(false).Times(1)
	th.mm.EXPECT().HasBallot(p.Ballot.ID()).Return(false).Times(1)
	for i, bid := range p.Votes.Support {
		th.mm.EXPECT().GetBlockLayer(bid).Return(p.LayerIndex.Sub(uint32(i+1)), nil)
	}
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotsPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), p.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlocksPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(nil).Times(1)
	th.mf.EXPECT().GetTxs(gomock.Any(), p.TxIDs).Return(nil).Times(1)
	th.mp.EXPECT().AddProposal(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, proposal *types.Proposal) error {
			require.Equal(t, p, proposal)
			return nil
		}).Times(1)
	errUnknown := errors.New("unknown")
	th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(errUnknown).Times(1)
	require.ErrorIs(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer), errUnknown)
}

func TestProposal_ValidProposal(t *testing.T) {
	th := createTestHandler(t)
	p := createProposal(t)
	data := encodeProposal(t, p)
	th.mp.EXPECT().HasProposal(p.ID()).Return(false).Times(1)
	th.mm.EXPECT().HasBallot(p.Ballot.ID()).Return(false).Times(1)
	for i, bid := range p.Votes.Support {
		th.mm.EXPECT().GetBlockLayer(bid).Return(p.LayerIndex.Sub(uint32(i+1)), nil)
	}
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotsPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), p.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlocksPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(nil).Times(1)
	th.mf.EXPECT().GetTxs(gomock.Any(), p.TxIDs).Return(nil).Times(1)
	th.mp.EXPECT().AddProposal(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, proposal *types.Proposal) error {
			require.Equal(t, p, proposal)
			return nil
		}).Times(1)
	th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(nil).Times(1)
	require.NoError(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer))
}

func TestMetrics(t *testing.T) {
	th := createTestHandler(t)
	p := createProposal(t)
	data := encodeProposal(t, p)
	th.mp.EXPECT().HasProposal(p.ID()).Return(false).Times(1)
	for i, bid := range p.Votes.Support {
		th.mm.EXPECT().GetBlockLayer(bid).Return(p.LayerIndex.Sub(uint32(i+1)), nil)
	}
	th.mm.EXPECT().HasBallot(p.Ballot.ID()).Return(false).Times(1)
	th.mf.EXPECT().GetBallots(gomock.Any(), []types.BallotID{p.Votes.Base, p.RefBallot}, p2p.AnyPeer).Return(nil).Times(1)
	th.mf.EXPECT().TrackBallotsPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mf.EXPECT().GetAtxs(gomock.Any(), types.ATXIDList{p.AtxID}).Return(nil).Times(1)
	th.mf.EXPECT().GetBlocks(gomock.Any(), p.Votes.Support).Return(nil).Times(1)
	th.mf.EXPECT().TrackBlocksPeer(gomock.Any(), p2p.AnyPeer, gomock.Any())
	th.mv.EXPECT().CheckEligibility(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, ballot *types.Ballot) (bool, error) {
			require.Equal(t, p.Ballot.ID(), ballot.ID())
			return true, nil
		})
	th.mm.EXPECT().AddBallot(&p.Ballot).Return(nil).Times(1)
	th.mf.EXPECT().GetTxs(gomock.Any(), p.TxIDs).Return(nil).Times(1)
	th.mp.EXPECT().AddProposal(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, proposal *types.Proposal) error {
			require.Equal(t, p, proposal)
			return nil
		}).Times(1)
	th.mm.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(nil).Times(1)
	require.NoError(t, th.HandleProposalData(context.TODO(), data, p2p.AnyPeer))
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
