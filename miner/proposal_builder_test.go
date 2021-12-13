package miner

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	dbmocks "github.com/spacemeshos/go-spacemesh/database/mocks"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/miner/mocks"
	pubsubmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/signing"
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
	ctrl    *gomock.Controller
	mRefDB  *dbmocks.MockDatabase
	mOracle *mocks.MockproposalOracle
	mAtxDB  *mocks.MockactivationDB
	mMesh   *mocks.MockmeshProvider
	mBaseBP *mocks.MockbaseBallotProvider
	mTxPool *mocks.MocktxPool
	mPubSub *pubsubmocks.MockPublisher
	mBeacon *smocks.MockBeaconGetter
	mSync   *smocks.MockSyncStateProvider
}

func createBuilder(tb testing.TB) *testBuilder {
	types.SetLayersPerEpoch(layersPerEpoch)
	nodeID, edSigner, vrfSigner := generateNodeIDAndSigner(tb)
	ctrl := gomock.NewController(tb)
	pb := &testBuilder{
		ctrl:    ctrl,
		mRefDB:  dbmocks.NewMockDatabase(ctrl),
		mOracle: mocks.NewMockproposalOracle(ctrl),
		mAtxDB:  mocks.NewMockactivationDB(ctrl),
		mMesh:   mocks.NewMockmeshProvider(ctrl),
		mBaseBP: mocks.NewMockbaseBallotProvider(ctrl),
		mTxPool: mocks.NewMocktxPool(ctrl),
		mPubSub: pubsubmocks.NewMockPublisher(ctrl),
		mBeacon: smocks.NewMockBeaconGetter(ctrl),
		mSync:   smocks.NewMockSyncStateProvider(ctrl),
	}
	mProjector := mocks.NewMockprojector(ctrl)
	mProjector.EXPECT().GetProjection(gomock.Any()).Return(uint64(1), uint64(1000), nil).AnyTimes()
	pb.ProposalBuilder = NewProposalBuilder(context.TODO(), make(chan types.LayerID), edSigner, vrfSigner,
		pb.mAtxDB, pb.mPubSub, pb.mMesh, pb.mBaseBP, pb.mBeacon, pb.mSync, mProjector, pb.mTxPool,
		WithLogger(logtest.New(tb)),
		WithLayerSize(20),
		WithLayerPerEpoch(3),
		WithTxsPerProposal(txCount),
		WithMinerID(nodeID),
		withRefDatabase(pb.mRefDB),
		withOracle(pb.mOracle))
	return pb
}

func genTX(tb testing.TB, nonce uint64, recipient types.Address, signer *signing.EdSigner) *types.Transaction {
	tb.Helper()
	tx, err := types.NewSignedTx(nonce, recipient, 1, defaultGasLimit, defaultFee, signer)
	require.NoError(tb, err)
	return tx
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
	defer b.ctrl.Finish()

	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(false).Times(1)
	b.mRefDB.EXPECT().Close().Times(1)

	require.NoError(t, b.Start(context.TODO()))
	// calling Start the second time should have no effect
	require.NoError(t, b.Start(context.TODO()))

	// causing it to build a block
	b.layerTimer <- types.NewLayerID(layersPerEpoch * 3)

	b.Close()
}

func TestBuilder_HandleLayer_MultipleProposals(t *testing.T) {
	b := createBuilder(t)
	defer b.ctrl.Finish()

	require.NoError(t, b.Start(context.TODO()))

	layerID := types.NewLayerID(layersPerEpoch * 3)
	epoch := layerID.GetEpoch()
	beacon := types.RandomBeacon()
	atxID := types.RandomATXID()
	activeSet := genActiveSet(t)
	numSlots := 2
	proofs := genProofs(t, numSlots)

	tx1 := genTX(t, 1, types.BytesToAddress([]byte{0x01}), signing.NewEdSigner())
	bb1 := types.RandomBallotID()

	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(atxID, activeSet, proofs, nil).Times(1)

	// for 1st proposal, containing the ref ballot of this epoch
	b.mTxPool.EXPECT().SelectTopNTransactions(gomock.Any(), gomock.Any()).Return([]types.TransactionID{tx1.ID()}, nil, nil).Times(1)
	b.mBaseBP.EXPECT().BaseBallot(gomock.Any()).Return(bb1, [][]types.BlockID{nil, nil, nil}, nil).Times(1)
	b.mRefDB.EXPECT().Get(getEpochKey(epoch)).Return(nil, database.ErrNotFound).Times(1)
	b.mRefDB.EXPECT().Put(getEpochKey(epoch), gomock.Any()).Return(nil).Times(1)
	b.mMesh.EXPECT().AddProposalWithTxs(gomock.Any(), gomock.Any()).Return(nil).Times(1)
	b.mPubSub.EXPECT().Publish(gomock.Any(), proposals.NewProposalProtocol, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, data []byte) error {
			var p types.Proposal
			require.NoError(t, types.BytesToInterface(data, &p))
			p.Initialize()
			assert.Equal(t, types.EmptyBallotID, p.RefBallot)
			assert.Equal(t, bb1, p.BaseBallot)
			assert.Equal(t, atxID, p.AtxID)
			require.NotNil(t, p.EpochData)
			assert.Equal(t, activeSet, p.EpochData.ActiveSet)
			assert.Equal(t, beacon, p.EpochData.Beacon)
			assert.Equal(t, []types.TransactionID{tx1.ID()}, p.TxIDs)
			return nil
		}).Times(1)

	// for the 2nd proposal
	refBid := types.RandomBallotID()
	tx2 := genTX(t, 1, types.BytesToAddress([]byte{0x02}), signing.NewEdSigner())
	bb2 := types.RandomBallotID()
	b.mTxPool.EXPECT().SelectTopNTransactions(gomock.Any(), gomock.Any()).Return([]types.TransactionID{tx2.ID()}, nil, nil).Times(1)
	b.mBaseBP.EXPECT().BaseBallot(gomock.Any()).Return(bb2, [][]types.BlockID{nil, nil, nil}, nil).Times(1)
	b.mRefDB.EXPECT().Get(getEpochKey(epoch)).Return(refBid.Bytes(), nil).Times(1)
	b.mMesh.EXPECT().AddProposalWithTxs(gomock.Any(), gomock.Any()).Return(nil).Times(1)
	b.mPubSub.EXPECT().Publish(gomock.Any(), proposals.NewProposalProtocol, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, data []byte) error {
			var p types.Proposal
			require.NoError(t, types.BytesToInterface(data, &p))
			p.Initialize()
			assert.Equal(t, bb2, p.BaseBallot)
			assert.Equal(t, refBid, p.RefBallot)
			assert.Equal(t, atxID, p.AtxID)
			require.Nil(t, p.EpochData)
			assert.Equal(t, []types.TransactionID{tx2.ID()}, p.TxIDs)
			return nil
		}).Times(1)

	assert.NoError(t, b.handleLayer(context.TODO(), layerID))

	b.mRefDB.EXPECT().Close().Times(1)
	b.Close()
}

func TestBuilder_HandleLayer_OneProposal(t *testing.T) {
	b := createBuilder(t)
	defer b.ctrl.Finish()

	require.NoError(t, b.Start(context.TODO()))

	layerID := types.NewLayerID(layersPerEpoch * 3)
	epoch := layerID.GetEpoch()
	beacon := types.RandomBeacon()
	atxID := types.RandomATXID()
	activeSet := genActiveSet(t)
	proofs := genProofs(t, 1)

	tx := genTX(t, 1, types.BytesToAddress([]byte{0x01}), signing.NewEdSigner())
	bb := types.RandomBallotID()

	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(atxID, activeSet, proofs, nil).Times(1)

	// for 1st proposal, containing the ref ballot of this epoch
	b.mTxPool.EXPECT().SelectTopNTransactions(gomock.Any(), gomock.Any()).Return([]types.TransactionID{tx.ID()}, nil, nil).Times(1)
	b.mBaseBP.EXPECT().BaseBallot(gomock.Any()).Return(bb, [][]types.BlockID{nil, nil, nil}, nil).Times(1)
	b.mRefDB.EXPECT().Get(getEpochKey(epoch)).Return(nil, database.ErrNotFound).Times(1)
	b.mRefDB.EXPECT().Put(getEpochKey(epoch), gomock.Any()).Return(nil).Times(1)
	b.mMesh.EXPECT().AddProposalWithTxs(gomock.Any(), gomock.Any()).Return(nil).Times(1)
	b.mPubSub.EXPECT().Publish(gomock.Any(), proposals.NewProposalProtocol, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, data []byte) error {
			var p types.Proposal
			require.NoError(t, types.BytesToInterface(data, &p))
			p.Initialize()
			assert.Equal(t, types.EmptyBallotID, p.RefBallot)
			assert.Equal(t, bb, p.BaseBallot)
			assert.Equal(t, atxID, p.AtxID)
			assert.NotNil(t, p.EpochData)
			assert.Equal(t, activeSet, p.EpochData.ActiveSet)
			assert.Equal(t, beacon, p.EpochData.Beacon)
			assert.Equal(t, []types.TransactionID{tx.ID()}, p.TxIDs)
			return nil
		}).Times(1)

	assert.NoError(t, b.handleLayer(context.TODO(), layerID))

	b.mRefDB.EXPECT().Close().Times(1)
	b.Close()
}

func TestBuilder_HandleLayer_Genesis(t *testing.T) {
	b := createBuilder(t)
	defer b.ctrl.Finish()

	layerID := types.NewLayerID(layersPerEpoch)
	assert.ErrorIs(t, b.handleLayer(context.TODO(), layerID), errGenesis)
}

func TestBuilder_HandleLayer_NotSynced(t *testing.T) {
	b := createBuilder(t)
	defer b.ctrl.Finish()

	layerID := types.NewLayerID(layersPerEpoch * 3)
	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(false).Times(1)

	assert.ErrorIs(t, b.handleLayer(context.TODO(), layerID), errNotSynced)
}

func TestBuilder_HandleLayer_NoBeacon(t *testing.T) {
	b := createBuilder(t)
	defer b.ctrl.Finish()

	layerID := types.NewLayerID(layersPerEpoch * 3)
	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.EmptyBeacon, errors.New("unknown")).Times(1)

	assert.ErrorIs(t, b.handleLayer(context.TODO(), layerID), errNoBeacon)
}

func TestBuilder_HandleLayer_EligibilityError(t *testing.T) {
	b := createBuilder(t)
	defer b.ctrl.Finish()

	layerID := types.NewLayerID(layersPerEpoch * 3)
	beacon := types.RandomBeacon()
	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	errUnknown := errors.New("unknown")
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(*types.EmptyATXID, nil, nil, errUnknown).Times(1)

	assert.ErrorIs(t, b.handleLayer(context.TODO(), layerID), errUnknown)
}

func TestBuilder_HandleLayer_NotEligible(t *testing.T) {
	b := createBuilder(t)
	defer b.ctrl.Finish()

	layerID := types.NewLayerID(layersPerEpoch * 3)
	beacon := types.RandomBeacon()
	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(types.RandomATXID(), genActiveSet(t), []types.VotingEligibilityProof{}, nil).Times(1)

	assert.NoError(t, b.handleLayer(context.TODO(), layerID))
}

func TestBuilder_HandleLayer_SelectTXError(t *testing.T) {
	b := createBuilder(t)
	defer b.ctrl.Finish()

	layerID := types.NewLayerID(layersPerEpoch * 3)
	beacon := types.RandomBeacon()
	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(types.RandomATXID(), genActiveSet(t), genProofs(t, 1), nil).Times(1)
	errUnknown := errors.New("unknown")
	b.mTxPool.EXPECT().SelectTopNTransactions(gomock.Any(), gomock.Any()).Return(nil, nil, errUnknown).Times(1)

	assert.ErrorIs(t, b.handleLayer(context.TODO(), layerID), errUnknown)
}

func TestBuilder_HandleLayer_BaseBlockError(t *testing.T) {
	b := createBuilder(t)
	defer b.ctrl.Finish()

	layerID := types.NewLayerID(layersPerEpoch * 3)
	beacon := types.RandomBeacon()
	tx := genTX(t, 1, types.BytesToAddress([]byte{0x01}), signing.NewEdSigner())

	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(types.RandomATXID(), genActiveSet(t), genProofs(t, 1), nil).Times(1)
	b.mTxPool.EXPECT().SelectTopNTransactions(gomock.Any(), gomock.Any()).Return([]types.TransactionID{tx.ID()}, nil, nil).Times(1)
	errUnknown := errors.New("unknown")
	b.mBaseBP.EXPECT().BaseBallot(gomock.Any()).Return(types.EmptyBallotID, nil, errUnknown).Times(1)

	assert.ErrorIs(t, b.handleLayer(context.TODO(), layerID), errUnknown)
}

func TestBuilder_HandleLayer_GetRefBallotError(t *testing.T) {
	b := createBuilder(t)
	defer b.ctrl.Finish()

	layerID := types.NewLayerID(layersPerEpoch * 3)
	epoch := layerID.GetEpoch()
	beacon := types.RandomBeacon()
	tx := genTX(t, 1, types.BytesToAddress([]byte{0x01}), signing.NewEdSigner())

	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(types.RandomATXID(), genActiveSet(t), genProofs(t, 1), nil).Times(1)
	b.mTxPool.EXPECT().SelectTopNTransactions(gomock.Any(), gomock.Any()).Return([]types.TransactionID{tx.ID()}, nil, nil).Times(1)
	b.mBaseBP.EXPECT().BaseBallot(gomock.Any()).Return(types.RandomBallotID(), [][]types.BlockID{nil, nil, nil}, nil).Times(1)
	errUnknown := errors.New("unknown")
	b.mRefDB.EXPECT().Get(getEpochKey(epoch)).Return(nil, errUnknown).Times(1)

	assert.ErrorIs(t, b.handleLayer(context.TODO(), layerID), errUnknown)
}

func TestBuilder_HandleLayer_SaveRefBallotError(t *testing.T) {
	b := createBuilder(t)
	defer b.ctrl.Finish()

	layerID := types.NewLayerID(layersPerEpoch * 3)
	epoch := layerID.GetEpoch()
	beacon := types.RandomBeacon()
	tx := genTX(t, 1, types.BytesToAddress([]byte{0x01}), signing.NewEdSigner())

	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(types.RandomATXID(), genActiveSet(t), genProofs(t, 1), nil).Times(1)
	b.mTxPool.EXPECT().SelectTopNTransactions(gomock.Any(), gomock.Any()).Return([]types.TransactionID{tx.ID()}, nil, nil).Times(1)
	b.mBaseBP.EXPECT().BaseBallot(gomock.Any()).Return(types.RandomBallotID(), [][]types.BlockID{nil, nil, nil}, nil).Times(1)
	b.mRefDB.EXPECT().Get(getEpochKey(epoch)).Return(nil, database.ErrNotFound).Times(1)
	errUnknown := errors.New("unknown")
	b.mRefDB.EXPECT().Put(getEpochKey(epoch), gomock.Any()).Return(errUnknown).Times(1)

	assert.ErrorIs(t, b.handleLayer(context.TODO(), layerID), errUnknown)
}

func TestBuilder_HandleLayer_CanceledDuringBuilding(t *testing.T) {
	b := createBuilder(t)
	defer b.ctrl.Finish()

	require.NoError(t, b.Start(context.TODO()))

	layerID := types.NewLayerID(layersPerEpoch * 3)
	epoch := layerID.GetEpoch()
	beacon := types.RandomBeacon()
	tx := genTX(t, 1, types.BytesToAddress([]byte{0x01}), signing.NewEdSigner())

	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(types.RandomATXID(), genActiveSet(t), genProofs(t, 1), nil).Times(1)
	b.mTxPool.EXPECT().SelectTopNTransactions(gomock.Any(), gomock.Any()).Return([]types.TransactionID{tx.ID()}, nil, nil).Times(1)
	b.mBaseBP.EXPECT().BaseBallot(gomock.Any()).Return(types.RandomBallotID(), [][]types.BlockID{nil, nil, nil}, nil).Times(1)
	b.mRefDB.EXPECT().Get(getEpochKey(epoch)).Return(nil, database.ErrNotFound).Times(1)
	b.mRefDB.EXPECT().Close().Times(1)

	b.Close()
	assert.NoError(t, b.handleLayer(context.TODO(), layerID))
}

func TestBuilder_HandleLayer_AddBlockError(t *testing.T) {
	b := createBuilder(t)
	defer b.ctrl.Finish()

	layerID := types.NewLayerID(layersPerEpoch * 3)
	epoch := layerID.GetEpoch()
	beacon := types.RandomBeacon()
	tx := genTX(t, 1, types.BytesToAddress([]byte{0x01}), signing.NewEdSigner())

	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(types.RandomATXID(), genActiveSet(t), genProofs(t, 1), nil).Times(1)
	b.mTxPool.EXPECT().SelectTopNTransactions(gomock.Any(), gomock.Any()).Return([]types.TransactionID{tx.ID()}, nil, nil).Times(1)
	b.mBaseBP.EXPECT().BaseBallot(gomock.Any()).Return(types.RandomBallotID(), [][]types.BlockID{nil, nil, nil}, nil).Times(1)
	b.mRefDB.EXPECT().Get(getEpochKey(epoch)).Return(nil, database.ErrNotFound).Times(1)
	b.mRefDB.EXPECT().Put(getEpochKey(epoch), gomock.Any()).Return(nil).Times(1)
	errUnknown := errors.New("unknown")
	b.mMesh.EXPECT().AddProposalWithTxs(gomock.Any(), gomock.Any()).Return(errUnknown).Times(1)

	assert.ErrorIs(t, b.handleLayer(context.TODO(), layerID), errUnknown)
}

func TestBuilder_HandleLayer_PublishError(t *testing.T) {
	b := createBuilder(t)
	defer b.ctrl.Finish()

	require.NoError(t, b.Start(context.TODO()))

	layerID := types.NewLayerID(layersPerEpoch * 3)
	epoch := layerID.GetEpoch()
	beacon := types.RandomBeacon()
	tx := genTX(t, 1, types.BytesToAddress([]byte{0x01}), signing.NewEdSigner())

	b.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).Times(1)
	b.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(beacon, nil).Times(1)
	b.mOracle.EXPECT().GetProposalEligibility(layerID, beacon).Return(types.RandomATXID(), genActiveSet(t), genProofs(t, 1), nil).Times(1)
	b.mTxPool.EXPECT().SelectTopNTransactions(gomock.Any(), gomock.Any()).Return([]types.TransactionID{tx.ID()}, nil, nil).Times(1)
	b.mBaseBP.EXPECT().BaseBallot(gomock.Any()).Return(types.RandomBallotID(), [][]types.BlockID{nil, nil, nil}, nil).Times(1)
	b.mRefDB.EXPECT().Get(getEpochKey(epoch)).Return(nil, database.ErrNotFound).Times(1)
	b.mRefDB.EXPECT().Put(getEpochKey(epoch), gomock.Any()).Return(nil).Times(1)
	b.mMesh.EXPECT().AddProposalWithTxs(gomock.Any(), gomock.Any()).Return(nil).Times(1)
	b.mPubSub.EXPECT().Publish(gomock.Any(), proposals.NewProposalProtocol, gomock.Any()).Return(errors.New("unknown")).Times(1)

	// publish error is ignored
	assert.NoError(t, b.handleLayer(context.TODO(), layerID))

	b.mRefDB.EXPECT().Close().Times(1)
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
	builder1.mBaseBP.EXPECT().BaseBallot(gomock.Any()).Return(types.RandomBallotID(), [][]types.BlockID{nil, nil, nil}, nil).Times(1)
	builder1.mRefDB.EXPECT().Get(getEpochKey(layerID.GetEpoch())).Return(types.RandomBallotID().Bytes(), nil).Times(1)
	b1, err := builder1.createProposal(context.TODO(), layerID, types.VotingEligibilityProof{}, atxID1, activeSet, beacon, nil)
	require.NoError(t, err)
	builder2.mBaseBP.EXPECT().BaseBallot(gomock.Any()).Return(types.RandomBallotID(), [][]types.BlockID{nil, nil, nil}, nil).Times(1)
	builder2.mRefDB.EXPECT().Get(getEpochKey(layerID.GetEpoch())).Return(types.RandomBallotID().Bytes(), nil).Times(1)
	b2, err := builder2.createProposal(context.TODO(), layerID, types.VotingEligibilityProof{}, atxID2, activeSet, beacon, nil)
	require.NoError(t, err)

	assert.NotEqual(t, b1.ID(), b2.ID())
}
