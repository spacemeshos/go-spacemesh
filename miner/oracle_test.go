package miner

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
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
}

func generateNodeIDAndSigner(tb testing.TB) (types.NodeID, *signing.EdSigner, *signing.VRFSigner) {
	tb.Helper()

	edSigner, err := signing.NewEdSigner()
	require.NoError(tb, err)
	vrfSigner, err := edSigner.VRFSigner(signing.WithNonceForNode(1, edSigner.NodeID()))
	require.NoError(tb, err)

	edPubkey := edSigner.PublicKey()
	nodeID := types.BytesToNodeID(edPubkey.Bytes())
	return nodeID, edSigner, vrfSigner
}

func genMinerATX(tb testing.TB, cdb *datastore.CachedDB, id types.ATXID, publishLayer types.LayerID, nodeID types.NodeID) *types.VerifiedActivationTx {
	atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
		NIPostChallenge: types.NIPostChallenge{
			PubLayerID: publishLayer,
		},
		NumUnits: defaultAtxWeight,
	}}
	atx.SetID(&id)
	atx.SetNodeID(&nodeID)
	vAtx, err := atx.Verify(0, 1)
	require.NoError(tb, err)
	require.NoError(tb, atxs.Add(cdb, vAtx, time.Now()))
	return vAtx
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
	bytes, err := codec.Encode(&ballot.InnerBallot)
	require.NoError(tb, err)
	ballot.Signature = signer.Sign(bytes)
	require.NoError(tb, ballot.Initialize())
	return ballot
}

func createTestOracle(tb testing.TB, layerSize, layersPerEpoch uint32) *testOracle {
	types.SetLayersPerEpoch(layersPerEpoch)

	lg := logtest.New(tb)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	nodeID, edSigner, vrfSigner := generateNodeIDAndSigner(tb)
	return &testOracle{
		Oracle:    newMinerOracle(layerSize, layersPerEpoch, cdb, vrfSigner, nodeID, lg),
		nodeID:    nodeID,
		edSigner:  edSigner,
		vrfSigner: vrfSigner,
	}
}

type epochATXInfo struct {
	atxID     types.ATXID
	activeSet []types.ATXID
	beacon    types.Beacon
}

func genATXForTargetEpochs(tb testing.TB, cdb *datastore.CachedDB, start, end types.EpochID, nodeID types.NodeID, layersPerEpoch uint32) map[types.EpochID]epochATXInfo {
	epochInfo := make(map[types.EpochID]epochATXInfo)
	for epoch := start; epoch < end; epoch++ {
		publishLayer := epoch.FirstLayer().Sub(layersPerEpoch)
		activeSet := types.RandomActiveSet(activeSetSize)
		info := epochATXInfo{
			beacon:    types.RandomBeacon(),
			activeSet: activeSet,
		}
		for i, id := range activeSet {
			nid := types.BytesToNodeID([]byte(strconv.Itoa(i)))
			if i == 0 {
				nid = nodeID
			}
			atx := genMinerATX(tb, cdb, id, publishLayer, nid)
			if i == 0 {
				info.atxID = atx.ID()
			}
		}
		epochInfo[epoch] = info
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
	mbc := smocks.NewMockBeaconCollector(gomock.NewController(t))
	validator := proposals.NewEligibilityValidator(layerSize, layersPerEpoch, o.cdb, mbc, nil, o.log.WithName("blkElgValidator"))

	startEpoch, numberOfEpochsToTest := uint32(2), uint32(2)
	startLayer := layersPerEpoch * startEpoch
	endLayer := types.NewLayerID(numberOfEpochsToTest * layersPerEpoch).Add(startLayer)
	counterValuesSeen := map[uint32]int{}
	epochInfo := genATXForTargetEpochs(t, o.cdb, types.EpochID(startEpoch), types.EpochID(startEpoch+numberOfEpochsToTest), o.nodeID, layersPerEpoch)
	for layer := types.NewLayerID(startLayer); layer.Before(endLayer); layer = layer.Add(1) {
		info, ok := epochInfo[layer.GetEpoch()]
		require.True(t, ok)
		_, _, proofs, err := o.GetProposalEligibility(layer, info.beacon)
		require.NoError(t, err)

		for _, proof := range proofs {
			b := genBallotWithEligibility(t, o.edSigner, layer, info.atxID, proof, info.activeSet, info.beacon)
			mbc.EXPECT().ReportBeaconFromBallot(layer.GetEpoch(), b, info.beacon, gomock.Any()).Times(1)
			eligible, err := validator.CheckEligibility(context.Background(), b)
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
	lid := types.NewLayerID(layersPerEpoch * 3)
	atxID, activeSet, proofs, err := o.GetProposalEligibility(lid, types.RandomBeacon())
	assert.ErrorIs(t, err, errMinerHasNoATXInPreviousEpoch)
	assert.Equal(t, *types.EmptyATXID, atxID)
	assert.Len(t, activeSet, 0)
	assert.Len(t, proofs, 0)
}

func TestOracle_ZeroEpochWeight(t *testing.T) {
	avgLayerSize := uint32(10)
	layersPerEpoch := uint32(20)
	o := createTestOracle(t, avgLayerSize, layersPerEpoch)
	lid := types.NewLayerID(layersPerEpoch * 3)
	atxID := types.RandomATXID()

	atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
		NIPostChallenge: types.NIPostChallenge{
			PubLayerID: (lid.GetEpoch() - 1).FirstLayer(),
		},
		NumUnits: 0,
	}}
	atx.SetID(&atxID)
	atx.SetNodeID(&o.nodeID)
	vAtx, err := atx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(o.cdb, vAtx, time.Now()))

	atxID, activeSet, proofs, err := o.GetProposalEligibility(lid, types.RandomBeacon())
	assert.ErrorIs(t, err, errZeroEpochWeight)
	assert.Equal(t, *types.EmptyATXID, atxID)
	assert.Len(t, activeSet, 0)
	assert.Len(t, proofs, 0)
}
