package miner

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/system/mocks"
)

const (
	defaultAtxWeight = 1024
	activeSetSize    = 10
)

type testOracle struct {
	*Oracle
	edSigner  *signing.EdSigner
	vrfSigner *signing.VRFSigner
}

func generateSigners(tb testing.TB) (*signing.EdSigner, *signing.VRFSigner) {
	tb.Helper()

	edSigner, err := signing.NewEdSigner()
	require.NoError(tb, err)
	vrfSigner, err := edSigner.VRFSigner()
	require.NoError(tb, err)

	return edSigner, vrfSigner
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
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now())
	vAtx, err := atx.Verify(0, 1, nil)
	require.NoError(tb, err)
	require.NoError(tb, atxs.Add(cdb, vAtx))
	return vAtx
}

func genBallotWithEligibility(tb testing.TB, signer *signing.EdSigner, lid types.LayerID, atxID types.ATXID, proof types.VotingEligibility, activeSet []types.ATXID, beacon types.Beacon) *types.Ballot {
	tb.Helper()
	ballot := &types.Ballot{
		BallotMetadata: types.BallotMetadata{
			Layer: lid,
		},
		InnerBallot: types.InnerBallot{
			AtxID: atxID,
			EpochData: &types.EpochData{
				ActiveSet: activeSet,
				Beacon:    beacon,
			},
		},
		EligibilityProofs: []types.VotingEligibility{proof},
	}
	require.NoError(tb, types.TestOnlySignAndInitBallot(ballot, signer))
	return ballot
}

func createTestOracle(tb testing.TB, layerSize, layersPerEpoch uint32) *testOracle {
	types.SetLayersPerEpoch(layersPerEpoch)

	lg := logtest.New(tb)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	edSigner, vrfSigner := generateSigners(tb)

	return &testOracle{
		Oracle:    newMinerOracle(layerSize, layersPerEpoch, cdb, vrfSigner, lg),
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

	ctrl := gomock.NewController(t)
	mbc := mocks.NewMockBeaconCollector(ctrl)
	vrfVerifier := proposals.NewMockvrfVerifier(ctrl)
	vrfVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).AnyTimes()

	nonceFetcher := proposals.NewMocknonceFetcher(ctrl)
	nonce := types.VRFPostIndex(rand.Uint64())

	validator := proposals.NewEligibilityValidator(layerSize, layersPerEpoch, o.cdb, mbc, nil, o.log.WithName("blkElgValidator"), vrfVerifier,
		proposals.WithNonceFetcher(nonceFetcher),
	)

	startEpoch, numberOfEpochsToTest := uint32(2), uint32(2)
	startLayer := layersPerEpoch * startEpoch
	endLayer := types.NewLayerID(numberOfEpochsToTest * layersPerEpoch).Add(startLayer)
	counterValuesSeen := map[uint32]int{}
	epochInfo := genATXForTargetEpochs(t, o.cdb, types.EpochID(startEpoch), types.EpochID(startEpoch+numberOfEpochsToTest), o.edSigner.NodeID(), layersPerEpoch)
	for layer := types.NewLayerID(startLayer); layer.Before(endLayer); layer = layer.Add(1) {
		info, ok := epochInfo[layer.GetEpoch()]
		require.True(t, ok)
		_, _, proofs, err := o.GetProposalEligibility(layer, info.beacon, nonce)
		require.NoError(t, err)

		for _, proof := range proofs {
			b := genBallotWithEligibility(t, o.edSigner, layer, info.atxID, proof, info.activeSet, info.beacon)
			mbc.EXPECT().ReportBeaconFromBallot(layer.GetEpoch(), b, info.beacon, gomock.Any()).Times(1)
			nonceFetcher.EXPECT().VRFNonce(b.SmesherID(), layer.GetEpoch()).Return(nonce, nil).Times(1)
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
	atxID, activeSet, proofs, err := o.GetProposalEligibility(lid, types.RandomBeacon(), types.VRFPostIndex(1))
	assert.ErrorIs(t, err, errMinerHasNoATXInPreviousEpoch)
	assert.Equal(t, *types.EmptyATXID, atxID)
	assert.Len(t, activeSet, 0)
	assert.Len(t, proofs, 0)
}
