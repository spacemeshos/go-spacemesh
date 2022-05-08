package miner

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/miner/mocks"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/signing"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const (
	defaultAtxWeight = 1024
	activeSetSize    = 10
)

type testOracle struct {
	*Oracle
	nodeID    types.NodeID
	edSigner  *signing.EdSigner
	vrfSigner *signing.VRFSigner
	ctrl      *gomock.Controller
	mdb       *mocks.MockactivationDB
}

func generateNodeIDAndSigner(tb testing.TB) (types.NodeID, *signing.EdSigner, *signing.VRFSigner) {
	tb.Helper()
	edSigner := signing.NewEdSigner()
	edPubkey := edSigner.PublicKey()
	nodeID := types.NodeID{
		Key: edPubkey.String(),
	}
	return nodeID, edSigner, edSigner.VRFSigner()
}

func genATXHeader(id types.ATXID) *types.ActivationTxHeader {
	atxHeader := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			StartTick: 0,
			EndTick:   1,
		},
		NumUnits: defaultAtxWeight,
	}
	atxHeader.SetID(&id)
	return atxHeader
}

func genMinerATXHeader(id types.ATXID, publishLayer types.LayerID, nodeID types.NodeID) *types.ActivationTxHeader {
	atxHeader := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			NodeID: types.NodeID{
				Key: nodeID.Key,
			},
			PubLayerID: publishLayer,
			StartTick:  0,
			EndTick:    1,
		},
		NumUnits: defaultAtxWeight,
	}
	atxHeader.SetID(&id)
	return atxHeader
}

func genBallotWithEligibility(tb testing.TB, signer *signing.EdSigner, lid types.LayerID, atxID types.ATXID, proof types.VotingEligibilityProof, activeSet []types.ATXID, beacon types.Beacon) *types.Ballot {
	tb.Helper()
	ballot := &types.Ballot{
		InnerBallot: types.InnerBallot{
			AtxID:             atxID,
			EligibilityProofs: []types.VotingEligibilityProof{proof},
			LayerIndex:        lid,
			EpochData: &types.EpochData{
				ActiveSet: activeSet,
				Beacon:    beacon,
			},
		},
	}
	bytes, err := codec.Encode(ballot.InnerBallot)
	require.NoError(tb, err)
	ballot.Signature = signer.Sign(bytes)
	ballot.Initialize()
	return ballot
}

func createTestOracle(tb testing.TB, layerSize, layersPerEpoch uint32) *testOracle {
	types.SetLayersPerEpoch(layersPerEpoch)

	ctrl := gomock.NewController(tb)
	mdb := mocks.NewMockactivationDB(ctrl)
	nodeID, edSigner, vrfSigner := generateNodeIDAndSigner(tb)
	return &testOracle{
		Oracle:    newMinerOracle(layerSize, layersPerEpoch, mdb, vrfSigner, nodeID, logtest.New(tb)),
		nodeID:    nodeID,
		edSigner:  edSigner,
		vrfSigner: vrfSigner,
		ctrl:      ctrl,
		mdb:       mdb,
	}
}

type epochATXInfo struct {
	atx       *types.ActivationTxHeader
	activeSet []types.ATXID
	beacon    types.Beacon
}

func genATXForTargetEpochs(tb testing.TB, start, end types.EpochID, nodeID types.NodeID, layersPerEpoch uint32) map[types.EpochID]epochATXInfo {
	epochInfo := make(map[types.EpochID]epochATXInfo)
	for epoch := start; epoch < end; epoch++ {
		activeSet := genActiveSet(tb)
		publishLayer := epoch.FirstLayer().Sub(layersPerEpoch)
		epochInfo[epoch] = epochATXInfo{
			atx:       genMinerATXHeader(types.RandomATXID(), publishLayer, nodeID),
			beacon:    types.RandomBeacon(),
			activeSet: activeSet,
		}
	}
	return epochInfo
}

func TestMinerOracle(t *testing.T) {
	// Happy flow with small numbers that can be inspected manually
	testMinerOracleAndProposalValidator(t, 10, 20)

	// Big, realistic numbers
	// testMinerOracleAndProposalValidator(t, 200, 4032) // commented out because it takes VERY long

	// More miners than blocks (ensure at least one block per activation)
	testMinerOracleAndProposalValidator(t, 2, 2)
}

func testMinerOracleAndProposalValidator(t *testing.T, layerSize uint32, layersPerEpoch uint32) {
	o := createTestOracle(t, layerSize, layersPerEpoch)
	defer o.ctrl.Finish()

	mbc := smocks.NewMockBeaconCollector(o.ctrl)
	mdb := mocks.NewMockactivationDB(o.ctrl)
	validator := proposals.NewEligibilityValidator(layerSize, layersPerEpoch, mdb, mbc, nil, o.log.WithName("blkElgValidator"))

	startEpoch, numberOfEpochsToTest := uint32(2), uint32(2)
	startLayer := layersPerEpoch * startEpoch
	endLayer := types.NewLayerID(numberOfEpochsToTest * layersPerEpoch).Add(startLayer)
	counterValuesSeen := map[uint32]int{}
	epochInfo := genATXForTargetEpochs(t, types.EpochID(startEpoch), types.EpochID(startEpoch+numberOfEpochsToTest), o.nodeID, layersPerEpoch)
	for epoch, info := range epochInfo {
		o.mdb.EXPECT().GetNodeAtxIDForEpoch(o.nodeID, epoch-1).Return(info.atx.ID(), nil).Times(1)
		o.mdb.EXPECT().GetAtxHeader(info.atx.ID()).Return(info.atx, nil).Times(1)
		o.mdb.EXPECT().GetEpochWeight(epoch).Return(uint64(activeSetSize*defaultAtxWeight), info.activeSet, nil).Times(1)
	}
	for layer := types.NewLayerID(startLayer); layer.Before(endLayer); layer = layer.Add(1) {
		info, ok := epochInfo[layer.GetEpoch()]
		require.True(t, ok)
		_, _, proofs, err := o.GetProposalEligibility(layer, info.beacon)
		require.NoError(t, err)

		for _, proof := range proofs {
			b := genBallotWithEligibility(t, o.edSigner, layer, info.atx.ID(), proof, info.activeSet, info.beacon)
			mbc.EXPECT().ReportBeaconFromBallot(layer.GetEpoch(), b.ID(), info.beacon, uint64(defaultAtxWeight)).Times(1)
			for _, atxID := range info.activeSet {
				mdb.EXPECT().GetAtxHeader(atxID).Return(genATXHeader(atxID), nil).Times(1)
			}
			mdb.EXPECT().GetAtxHeader(info.atx.ID()).Return(info.atx, nil).Times(1)
			eligible, err := validator.CheckEligibility(context.TODO(), b)
			require.NoError(t, err, "at layer %d, with layersPerEpoch %d", layer, layersPerEpoch)
			assert.True(t, eligible, "should be eligible at layer %d, but isn't", layer)
			counterValuesSeen[proof.J]++
		}
	}

	numberOfEligibleBallots := layerSize * layersPerEpoch / 10
	if numberOfEligibleBallots == 0 {
		numberOfEligibleBallots = 1
	}
	for c := uint32(0); c < numberOfEligibleBallots; c++ {
		assert.EqualValues(t, numberOfEpochsToTest, counterValuesSeen[c],
			"counter value %d expected %d times, but received %d times",
			c, numberOfEpochsToTest, counterValuesSeen[c])
	}
	assert.Len(t, counterValuesSeen, int(numberOfEligibleBallots))
}

func TestOracle_OwnATXNotFound(t *testing.T) {
	avgLayerSize := uint32(10)
	layersPerEpoch := uint32(20)
	o := createTestOracle(t, avgLayerSize, layersPerEpoch)
	defer o.ctrl.Finish()

	lid := types.NewLayerID(layersPerEpoch * 3)
	o.mdb.EXPECT().GetNodeAtxIDForEpoch(o.nodeID, lid.GetEpoch()-1).Return(*types.EmptyATXID, database.ErrNotFound).Times(1)
	atxID, activeSet, proofs, err := o.GetProposalEligibility(lid, types.RandomBeacon())
	assert.ErrorIs(t, err, errMinerHasNoATXInPreviousEpoch)
	assert.Equal(t, *types.EmptyATXID, atxID)
	assert.Len(t, activeSet, 0)
	assert.Len(t, proofs, 0)
}

func TestOracle_OwnATXIDError(t *testing.T) {
	avgLayerSize := uint32(10)
	layersPerEpoch := uint32(20)
	o := createTestOracle(t, avgLayerSize, layersPerEpoch)
	defer o.ctrl.Finish()

	lid := types.NewLayerID(layersPerEpoch * 3)
	errUnknown := errors.New("unknown")
	o.mdb.EXPECT().GetNodeAtxIDForEpoch(o.nodeID, lid.GetEpoch()-1).Return(*types.EmptyATXID, errUnknown).Times(1)
	atxID, activeSet, proofs, err := o.GetProposalEligibility(lid, types.RandomBeacon())
	assert.ErrorIs(t, err, errUnknown)
	assert.Equal(t, *types.EmptyATXID, atxID)
	assert.Len(t, activeSet, 0)
	assert.Len(t, proofs, 0)
}

func TestOracle_OwnATXError(t *testing.T) {
	avgLayerSize := uint32(10)
	layersPerEpoch := uint32(20)
	o := createTestOracle(t, avgLayerSize, layersPerEpoch)
	defer o.ctrl.Finish()

	lid := types.NewLayerID(layersPerEpoch * 3)
	atxID := types.RandomATXID()
	errUnknown := errors.New("unknown")
	o.mdb.EXPECT().GetNodeAtxIDForEpoch(o.nodeID, lid.GetEpoch()-1).Return(atxID, nil).Times(1)
	o.mdb.EXPECT().GetAtxHeader(atxID).Return(nil, errUnknown).Times(1)
	atxID, activeSet, proofs, err := o.GetProposalEligibility(lid, types.RandomBeacon())
	assert.ErrorIs(t, err, errUnknown)
	assert.Equal(t, *types.EmptyATXID, atxID)
	assert.Len(t, activeSet, 0)
	assert.Len(t, proofs, 0)
}

func TestOracle_ZeroEpochWeight(t *testing.T) {
	avgLayerSize := uint32(10)
	layersPerEpoch := uint32(20)
	o := createTestOracle(t, avgLayerSize, layersPerEpoch)
	defer o.ctrl.Finish()

	lid := types.NewLayerID(layersPerEpoch * 3)
	atxID := types.RandomATXID()
	atx := genMinerATXHeader(atxID, lid.Sub(layersPerEpoch), o.nodeID)
	o.mdb.EXPECT().GetNodeAtxIDForEpoch(o.nodeID, lid.GetEpoch()-1).Return(atxID, nil).Times(1)
	o.mdb.EXPECT().GetAtxHeader(atxID).Return(atx, nil).Times(1)
	o.mdb.EXPECT().GetEpochWeight(lid.GetEpoch()).Return(uint64(0), nil, nil)
	atxID, activeSet, proofs, err := o.GetProposalEligibility(lid, types.RandomBeacon())
	assert.ErrorIs(t, err, errZeroEpochWeight)
	assert.Equal(t, *types.EmptyATXID, atxID)
	assert.Len(t, activeSet, 0)
	assert.Len(t, proofs, 0)
}

func TestOracle_EmptyActiveSet(t *testing.T) {
	avgLayerSize := uint32(10)
	layersPerEpoch := uint32(20)
	o := createTestOracle(t, avgLayerSize, layersPerEpoch)
	defer o.ctrl.Finish()

	lid := types.NewLayerID(layersPerEpoch * 3)
	atxID := types.RandomATXID()
	atx := genMinerATXHeader(atxID, lid.Sub(layersPerEpoch), o.nodeID)
	o.mdb.EXPECT().GetNodeAtxIDForEpoch(o.nodeID, lid.GetEpoch()-1).Return(atxID, nil).Times(1)
	o.mdb.EXPECT().GetAtxHeader(atxID).Return(atx, nil).Times(1)
	o.mdb.EXPECT().GetEpochWeight(lid.GetEpoch()).Return(uint64(defaultAtxWeight), nil, nil)
	atxID, activeSet, proofs, err := o.GetProposalEligibility(lid, types.RandomBeacon())
	assert.ErrorIs(t, err, errEmptyActiveSet)
	assert.Equal(t, *types.EmptyATXID, atxID)
	assert.Len(t, activeSet, 0)
	assert.Len(t, proofs, 0)
}
