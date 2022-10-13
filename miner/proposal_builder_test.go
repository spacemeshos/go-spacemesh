package miner

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/miner/mocks"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	pubsubmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const (
	layersPerEpoch  = 3
	txCount         = 100
	defaultGasLimit = 10
	defaultFee      = 1
)

type testBuilder struct {
	*ProposalBuilder
	mOracle *mocks.MockproposalOracle
	mBaseBP *mocks.MockvotesEncoder
	mCState *mocks.MockconservativeState
	mPubSub *pubsubmocks.MockPublisher
	mBeacon *smocks.MockBeaconGetter
	mSync   *smocks.MockSyncStateProvider
}

func createBuilder(tb testing.TB) *testBuilder {
	types.SetLayersPerEpoch(layersPerEpoch)
	nodeID, edSigner, vrfSigner := generateNodeIDAndSigner(tb)
	ctrl := gomock.NewController(tb)
	pb := &testBuilder{
		mOracle: mocks.NewMockproposalOracle(ctrl),
		mBaseBP: mocks.NewMockvotesEncoder(ctrl),
		mCState: mocks.NewMockconservativeState(ctrl),
		mPubSub: pubsubmocks.NewMockPublisher(ctrl),
		mBeacon: smocks.NewMockBeaconGetter(ctrl),
		mSync:   smocks.NewMockSyncStateProvider(ctrl),
	}
	lg := logtest.New(tb)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	pb.ProposalBuilder = NewProposalBuilder(context.TODO(), make(chan types.LayerID), edSigner, vrfSigner,
		cdb, pb.mPubSub, pb.mBaseBP, pb.mBeacon, pb.mSync, pb.mCState,
		WithLogger(lg),
		WithLayerSize(20),
		WithLayerPerEpoch(3),
		WithTxsPerProposal(txCount),
		WithMinerID(nodeID),
		withOracle(pb.mOracle))
	return pb
}

func genTX(tb testing.TB, nonce uint64, recipient types.Address, signer *signing.EdSigner) *types.Transaction {
	tb.Helper()

	raw := wallet.Spend(signer.PrivateKey(), recipient, defaultFee,
		types.Nonce{Counter: nonce},
	)
	tx := types.Transaction{
		RawTx:    types.NewRawTx(raw),
		TxHeader: &types.TxHeader{},
	}
	tx.MaxGas = defaultGasLimit
	tx.MaxSpend = defaultFee
	tx.GasPrice = 1
	tx.Nonce = types.Nonce{Counter: nonce}
	tx.Principal = types.GenerateAddress(signer.PublicKey().Bytes())
	return &tx
}

func genActiveSet(tb testing.TB) []types.ATXID {
	tb.Helper()
	activeSet := make([]types.ATXID, 0, activeSetSize)
	for i := 0; i < activeSetSize; i++ {
		activeSet = append(activeSet, types.RandomATXID())
	}
	return activeSet
}

func genProofs(tb testing.TB, size int) []types.VotingEligibilityProof {
	tb.Helper()
	proofs := make([]types.VotingEligibilityProof, 0, size)
	for i := 0; i < size; i++ {
		proofs = append(proofs, types.VotingEligibilityProof{J: uint32(i)})
	}
	return proofs
}

func TestBuilder_StartAndClose(t *testing.T) {
	b := createBuilder(t)

	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(false).Times(1)

	require.NoError(t, b.Start(context.TODO()))
	// calling Start the second time should have no effect
	require.NoError(t, b.Start(context.TODO()))

	// causing it to build a block
	b.layerTimer <- types.NewLayerID(layersPerEpoch * 3)

	b.Close()
}

func TestBuilder_HandleLayer_MultipleProposals(t *testing.T) {
	b := createBuilder(t)

	require.NoError(t, b.Start(context.TODO()))

	layerID := types.NewLayerID(layersPerEpoch * 3)
	beacon := types.RandomBeacon()
	atxID := types.RandomATXID()
	activeSet := genActiveSet(t)
	numSlots := 2
	proofs := genProofs(t, numSlots)

	tx1 := genTX(t, 1, types.GenerateAddress([]byte{0x01}), signing.NewEdSigner())
	base := types.RandomBallotID()

	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(atxID, activeSet, proofs, nil).Times(1)

	// for 1st proposal, containing the ref ballot of this epoch
	b.mCState.EXPECT().SelectProposalTXs(layerID, len(proofs)).Return([]types.TransactionID{tx1.ID}).Times(1)
	b.mBaseBP.EXPECT().TallyVotes(gomock.Any(), gomock.Any())
	b.mBaseBP.EXPECT().EncodeVotes(gomock.Any(), gomock.Any()).Return(&types.Votes{Base: base}, nil).Times(1)
	meshHash := types.RandomHash()
	require.NoError(t, layers.SetHashes(b.cdb, layerID.Sub(1), types.RandomHash(), meshHash))
	b.mPubSub.EXPECT().Publish(gomock.Any(), pubsub.ProposalProtocol, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, data []byte) error {
			var p types.Proposal
			require.NoError(t, codec.Decode(data, &p))
			require.NoError(t, p.Initialize())
			require.Equal(t, types.EmptyBallotID, p.RefBallot)
			require.Equal(t, base, p.Votes.Base)
			require.Equal(t, atxID, p.AtxID)
			require.Equal(t, p.LayerIndex, layerID)
			require.NotNil(t, p.EpochData)
			require.Equal(t, activeSet, p.EpochData.ActiveSet)
			require.Equal(t, beacon, p.EpochData.Beacon)
			require.Equal(t, []types.TransactionID{tx1.ID}, p.TxIDs)
			require.Equal(t, proofs, p.EligibilityProofs)
			require.Equal(t, meshHash, p.MeshHash)
			return nil
		}).Times(1)

	require.NoError(t, b.handleLayer(context.TODO(), layerID))

	b.Close()
}

func TestBuilder_HandleLayer_OneProposal(t *testing.T) {
	b := createBuilder(t)

	require.NoError(t, b.Start(context.TODO()))

	layerID := types.NewLayerID(layersPerEpoch * 3)
	beacon := types.RandomBeacon()
	atxID := types.RandomATXID()
	activeSet := genActiveSet(t)
	proofs := genProofs(t, 1)

	tx := genTX(t, 1, types.GenerateAddress([]byte{0x01}), signing.NewEdSigner())
	bb := types.RandomBallotID()

	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(atxID, activeSet, proofs, nil).Times(1)

	// for 1st proposal, containing the ref ballot of this epoch
	b.mCState.EXPECT().SelectProposalTXs(layerID, len(proofs)).Return([]types.TransactionID{tx.ID}).Times(1)

	b.mBaseBP.EXPECT().TallyVotes(gomock.Any(), gomock.Any())
	b.mBaseBP.EXPECT().EncodeVotes(gomock.Any(), gomock.Any()).Return(&types.Votes{Base: bb}, nil).Times(1)
	meshHash := types.RandomHash()
	require.NoError(t, layers.SetHashes(b.cdb, layerID.Sub(1), types.RandomHash(), meshHash))
	b.mPubSub.EXPECT().Publish(gomock.Any(), pubsub.ProposalProtocol, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, data []byte) error {
			var p types.Proposal
			require.NoError(t, codec.Decode(data, &p))
			require.NoError(t, p.Initialize())
			require.Equal(t, types.EmptyBallotID, p.RefBallot)
			require.Equal(t, bb, p.Votes.Base)
			require.Equal(t, atxID, p.AtxID)
			require.Equal(t, p.LayerIndex, layerID)
			require.NotNil(t, p.EpochData)
			require.Equal(t, activeSet, p.EpochData.ActiveSet)
			require.Equal(t, beacon, p.EpochData.Beacon)
			require.Equal(t, []types.TransactionID{tx.ID}, p.TxIDs)
			require.Equal(t, meshHash, p.MeshHash)
			return nil
		}).Times(1)

	require.NoError(t, b.handleLayer(context.TODO(), layerID))

	b.Close()
}

func TestBuilder_HandleLayer_Genesis(t *testing.T) {
	b := createBuilder(t)

	layerID := types.NewLayerID(layersPerEpoch)
	require.ErrorIs(t, b.handleLayer(context.TODO(), layerID), errGenesis)
}

func TestBuilder_HandleLayer_NotSynced(t *testing.T) {
	b := createBuilder(t)

	layerID := types.NewLayerID(layersPerEpoch * 3)
	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(false).Times(1)

	require.ErrorIs(t, b.handleLayer(context.TODO(), layerID), errNotSynced)
}

func TestBuilder_HandleLayer_NoBeacon(t *testing.T) {
	b := createBuilder(t)

	layerID := types.NewLayerID(layersPerEpoch * 3)
	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.EmptyBeacon, errors.New("unknown")).Times(1)

	require.ErrorIs(t, b.handleLayer(context.TODO(), layerID), errNoBeacon)
}

func TestBuilder_HandleLayer_EligibilityError(t *testing.T) {
	b := createBuilder(t)

	layerID := types.NewLayerID(layersPerEpoch * 3)
	beacon := types.RandomBeacon()
	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	errUnknown := errors.New("unknown")
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(*types.EmptyATXID, nil, nil, errUnknown).Times(1)

	require.ErrorIs(t, b.handleLayer(context.TODO(), layerID), errUnknown)
}

func TestBuilder_HandleLayer_NotEligible(t *testing.T) {
	b := createBuilder(t)

	layerID := types.NewLayerID(layersPerEpoch * 3)
	beacon := types.RandomBeacon()
	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(types.RandomATXID(), genActiveSet(t), []types.VotingEligibilityProof{}, nil).Times(1)

	require.NoError(t, b.handleLayer(context.TODO(), layerID))
}

func TestBuilder_HandleLayer_BaseBlockError(t *testing.T) {
	b := createBuilder(t)

	layerID := types.NewLayerID(layersPerEpoch * 3)
	beacon := types.RandomBeacon()

	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(types.RandomATXID(), genActiveSet(t), genProofs(t, 1), nil).Times(1)
	errUnknown := errors.New("unknown")
	b.mBaseBP.EXPECT().TallyVotes(gomock.Any(), gomock.Any())
	b.mBaseBP.EXPECT().EncodeVotes(gomock.Any(), gomock.Any()).Return(nil, errUnknown).Times(1)

	require.ErrorIs(t, b.handleLayer(context.TODO(), layerID), errUnknown)
}

func TestBuilder_HandleLayer_NoRefBallot(t *testing.T) {
	b := createBuilder(t)

	layerID := types.NewLayerID(layersPerEpoch * 3)
	beacon := types.RandomBeacon()
	activeSet := genActiveSet(t)
	tx := genTX(t, 1, types.GenerateAddress([]byte{0x01}), signing.NewEdSigner())

	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(types.RandomATXID(), activeSet, genProofs(t, 1), nil).Times(1)
	b.mCState.EXPECT().SelectProposalTXs(layerID, 1).Return([]types.TransactionID{tx.ID}).Times(1)
	b.mBaseBP.EXPECT().TallyVotes(gomock.Any(), gomock.Any())
	b.mBaseBP.EXPECT().EncodeVotes(gomock.Any(), gomock.Any()).Return(&types.Votes{Base: types.RandomBallotID()}, nil).Times(1)
	require.NoError(t, layers.SetHashes(b.cdb, layerID.Sub(1), types.RandomHash(), types.RandomHash()))
	b.mPubSub.EXPECT().Publish(gomock.Any(), pubsub.ProposalProtocol, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, data []byte) error {
			var got types.Proposal
			require.NoError(t, codec.Decode(data, &got))
			require.Equal(t, types.EmptyBallotID, got.RefBallot)
			require.Equal(t, types.EpochData{ActiveSet: activeSet, Beacon: beacon}, *got.EpochData)
			return nil
		}).Times(1)

	require.NoError(t, b.handleLayer(context.TODO(), layerID))
	b.Close()
}

func TestBuilder_HandleLayer_RefBallot(t *testing.T) {
	b := createBuilder(t)

	layerID := types.NewLayerID(layersPerEpoch * 3).Add(1)
	refBallot := types.NewExistingBallot(
		types.BallotID{1}, nil, b.ProposalBuilder.signer.PublicKey().Bytes(), types.InnerBallot{LayerIndex: layerID.Sub(1)})
	require.NoError(t, ballots.Add(b.cdb, &refBallot))
	beacon := types.RandomBeacon()
	tx := genTX(t, 1, types.GenerateAddress([]byte{0x01}), signing.NewEdSigner())

	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(types.RandomATXID(), genActiveSet(t), genProofs(t, 1), nil).Times(1)
	b.mCState.EXPECT().SelectProposalTXs(layerID, 1).Return([]types.TransactionID{tx.ID}).Times(1)
	b.mBaseBP.EXPECT().TallyVotes(gomock.Any(), gomock.Any())
	b.mBaseBP.EXPECT().EncodeVotes(gomock.Any(), gomock.Any()).Return(&types.Votes{Base: types.RandomBallotID()}, nil).Times(1)
	require.NoError(t, layers.SetHashes(b.cdb, layerID.Sub(1), types.RandomHash(), types.RandomHash()))
	b.mPubSub.EXPECT().Publish(gomock.Any(), pubsub.ProposalProtocol, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, data []byte) error {
			var got types.Proposal
			require.NoError(t, codec.Decode(data, &got))
			require.Equal(t, refBallot.ID(), got.RefBallot)
			require.Nil(t, got.EpochData)
			return nil
		}).Times(1)

	require.NoError(t, b.handleLayer(context.TODO(), layerID))
	b.Close()
}

func TestBuilder_HandleLayer_CanceledDuringBuilding(t *testing.T) {
	b := createBuilder(t)

	require.NoError(t, b.Start(context.TODO()))

	layerID := types.NewLayerID(layersPerEpoch * 3)
	beacon := types.RandomBeacon()
	tx := genTX(t, 1, types.GenerateAddress([]byte{0x01}), signing.NewEdSigner())

	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(types.RandomATXID(), genActiveSet(t), genProofs(t, 1), nil).Times(1)
	b.mCState.EXPECT().SelectProposalTXs(layerID, 1).Return([]types.TransactionID{tx.ID}).Times(1)
	b.mBaseBP.EXPECT().TallyVotes(gomock.Any(), gomock.Any())
	b.mBaseBP.EXPECT().EncodeVotes(gomock.Any(), gomock.Any()).Return(&types.Votes{Base: types.RandomBallotID()}, nil).Times(1)
	require.NoError(t, layers.SetHashes(b.cdb, layerID.Sub(1), types.RandomHash(), types.RandomHash()))

	b.Close()
	require.NoError(t, b.handleLayer(context.TODO(), layerID))
}

func TestBuilder_HandleLayer_PublishError(t *testing.T) {
	b := createBuilder(t)

	require.NoError(t, b.Start(context.TODO()))

	layerID := types.NewLayerID(layersPerEpoch * 3)
	beacon := types.RandomBeacon()
	tx := genTX(t, 1, types.GenerateAddress([]byte{0x01}), signing.NewEdSigner())

	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(types.RandomATXID(), genActiveSet(t), genProofs(t, 1), nil).Times(1)
	b.mCState.EXPECT().SelectProposalTXs(layerID, 1).Return([]types.TransactionID{tx.ID}).Times(1)
	b.mBaseBP.EXPECT().TallyVotes(gomock.Any(), gomock.Any())
	b.mBaseBP.EXPECT().EncodeVotes(gomock.Any(), gomock.Any()).Return(&types.Votes{Base: types.RandomBallotID()}, nil).Times(1)
	require.NoError(t, layers.SetHashes(b.cdb, layerID.Sub(1), types.RandomHash(), types.RandomHash()))
	b.mPubSub.EXPECT().Publish(gomock.Any(), pubsub.ProposalProtocol, gomock.Any()).Return(errors.New("unknown")).Times(1)

	// publish error is ignored
	require.NoError(t, b.handleLayer(context.TODO(), layerID))
	b.Close()
}

func TestBuilder_HandleLayer_StateRootErrorOK(t *testing.T) {
	b := createBuilder(t)

	require.NoError(t, b.Start(context.TODO()))

	layerID := types.NewLayerID(layersPerEpoch * 3)
	beacon := types.RandomBeacon()
	tx := genTX(t, 1, types.GenerateAddress([]byte{0x01}), signing.NewEdSigner())

	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(types.RandomATXID(), genActiveSet(t), genProofs(t, 1), nil).Times(1)
	b.mCState.EXPECT().SelectProposalTXs(layerID, 1).Return([]types.TransactionID{tx.ID}).Times(1)
	b.mBaseBP.EXPECT().TallyVotes(gomock.Any(), gomock.Any())
	b.mBaseBP.EXPECT().EncodeVotes(gomock.Any(), gomock.Any()).Return(&types.Votes{Base: types.RandomBallotID()}, nil).Times(1)
	require.NoError(t, layers.SetHashes(b.cdb, layerID.Sub(1), types.RandomHash(), types.RandomHash()))
	b.mPubSub.EXPECT().Publish(gomock.Any(), pubsub.ProposalProtocol, gomock.Any()).Return(errors.New("unknown")).Times(1)

	require.NoError(t, b.handleLayer(context.TODO(), layerID))
	b.Close()
}

func TestBuilder_HandleLayer_MeshHashErrorOK(t *testing.T) {
	b := createBuilder(t)

	require.NoError(t, b.Start(context.TODO()))

	layerID := types.NewLayerID(layersPerEpoch * 3)
	beacon := types.RandomBeacon()
	tx := genTX(t, 1, types.GenerateAddress([]byte{0x01}), signing.NewEdSigner())

	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(types.RandomATXID(), genActiveSet(t), genProofs(t, 1), nil).Times(1)
	b.mCState.EXPECT().SelectProposalTXs(layerID, 1).Return([]types.TransactionID{tx.ID}).Times(1)
	b.mBaseBP.EXPECT().TallyVotes(gomock.Any(), gomock.Any())
	b.mBaseBP.EXPECT().EncodeVotes(gomock.Any(), gomock.Any()).Return(&types.Votes{Base: types.RandomBallotID()}, nil).Times(1)
	b.mPubSub.EXPECT().Publish(gomock.Any(), pubsub.ProposalProtocol, gomock.Any()).Return(errors.New("unknown")).Times(1)

	require.NoError(t, b.handleLayer(context.TODO(), layerID))
	b.Close()
}

func TestBuilder_UniqueBlockID(t *testing.T) {
	layerID := types.NewLayerID(layersPerEpoch * 3)

	builder1 := createBuilder(t)
	builder2 := createBuilder(t)
	atxID1 := types.RandomATXID()
	atxID2 := types.RandomATXID()
	activeSet := genActiveSet(t)
	beacon := types.RandomBeacon()
	meshHash := types.RandomHash()

	require.NoError(t, layers.SetHashes(builder1.cdb, layerID.Sub(1), types.RandomHash(), meshHash))
	b1, err := builder1.createProposal(context.TODO(), layerID, nil, atxID1, activeSet, beacon, nil, types.Votes{})
	require.NoError(t, err)

	require.NoError(t, layers.SetHashes(builder2.cdb, layerID.Sub(1), types.RandomHash(), meshHash))
	b2, err := builder2.createProposal(context.TODO(), layerID, nil, atxID2, activeSet, beacon, nil, types.Votes{})
	require.NoError(t, err)

	require.NotEqual(t, b1.ID(), b2.ID())
}
