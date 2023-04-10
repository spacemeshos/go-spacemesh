package miner

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
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
	nodeID    types.NodeID
	edSigner  *signing.EdSigner
	vrfSigner *signing.VRFSigner
}

func generateNodeIDAndSigner(tb testing.TB) (types.NodeID, *signing.EdSigner, *signing.VRFSigner) {
	tb.Helper()

	edSigner, err := signing.NewEdSigner()
	require.NoError(tb, err)
	vrfSigner, err := edSigner.VRFSigner()
	require.NoError(tb, err)

	edPubkey := edSigner.PublicKey()
	nodeID := types.BytesToNodeID(edPubkey.Bytes())
	return nodeID, edSigner, vrfSigner
}

func genMinerATX(tb testing.TB, cdb *datastore.CachedDB, id types.ATXID, publishLayer types.LayerID, signer *signing.EdSigner) *types.VerifiedActivationTx {
	atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
		NIPostChallenge: types.NIPostChallenge{
			PublishEpoch: publishLayer.GetEpoch(),
		},
		NumUnits: defaultAtxWeight,
	}}
	atx.SetID(id)
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now())
	activation.SignAndFinalizeAtx(signer, atx)
	vAtx, err := atx.Verify(0, 1)
	require.NoError(tb, err)
	require.NoError(tb, atxs.Add(cdb, vAtx))
	return vAtx
}

func genBallotWithEligibility(
	tb testing.TB,
	signer *signing.EdSigner,
	beacon types.Beacon,
	lid types.LayerID,
	ee *EpochEligibility,
) *types.Ballot {
	tb.Helper()
	ballot := &types.Ballot{
		InnerBallot: types.InnerBallot{
			Layer: lid,
			AtxID: ee.Atx,
			EpochData: &types.EpochData{
				ActiveSetHash:    ee.ActiveSet.Hash(),
				Beacon:           beacon,
				EligibilityCount: ee.Slots,
			},
		},
		ActiveSet:         ee.ActiveSet,
		EligibilityProofs: ee.Proofs[lid],
	}
	ballot.Signature = signer.Sign(signing.BALLOT, ballot.SignedBytes())
	ballot.SmesherID = signer.NodeID()
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

func genATXForTargetEpochs(tb testing.TB, cdb *datastore.CachedDB, start, end types.EpochID, signer *signing.EdSigner, layersPerEpoch uint32) map[types.EpochID]epochATXInfo {
	epochInfo := make(map[types.EpochID]epochATXInfo)
	for epoch := start; epoch < end; epoch++ {
		publishLayer := epoch.FirstLayer().Sub(layersPerEpoch)
		activeSet := types.RandomActiveSet(activeSetSize)
		atx := genMinerATX(tb, cdb, activeSet[0], publishLayer, signer)
		info := epochATXInfo{
			beacon:    types.RandomBeacon(),
			activeSet: activeSet,
			atxID:     atx.ID(),
		}
		for _, id := range activeSet[1:] {
			signer, err := signing.NewEdSigner()
			require.NoError(tb, err)
			genMinerATX(tb, cdb, id, publishLayer, signer)
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
	endLayer := types.LayerID(numberOfEpochsToTest * layersPerEpoch).Add(startLayer)
	counterValuesSeen := map[uint32]int{}
	epochInfo := genATXForTargetEpochs(t, o.cdb, types.EpochID(startEpoch), types.EpochID(startEpoch+numberOfEpochsToTest), o.edSigner, layersPerEpoch)
	for layer := types.LayerID(startLayer); layer.Before(endLayer); layer = layer.Add(1) {
		info, ok := epochInfo[layer.GetEpoch()]
		require.True(t, ok)
		ee, err := o.GetProposalEligibility(layer, info.beacon, nonce)
		require.NoError(t, err)

		for _, proof := range ee.Proofs[layer] {
			b := genBallotWithEligibility(t, o.edSigner, info.beacon, layer, ee)
			b.SmesherID = o.edSigner.NodeID()
			mbc.EXPECT().ReportBeaconFromBallot(layer.GetEpoch(), b, info.beacon, gomock.Any()).Times(1)
			nonceFetcher.EXPECT().VRFNonce(b.SmesherID, layer.GetEpoch()).Return(nonce, nil).Times(1)
			eligible, err := validator.CheckEligibility(context.Background(), b)
			require.NoError(t, err, "at layer %d, with layersPerEpoch %d", layer, layersPerEpoch)
			require.True(t, eligible, "should be eligible at layer %d, but isn't", layer)
			counterValuesSeen[proof.J]++
		}
	}

	numberOfEligibleBallots := layerSize * layersPerEpoch / 10
	if numberOfEligibleBallots == 0 {
		numberOfEligibleBallots = 1
	}
	for c := uint32(0); c < numberOfEligibleBallots; c++ {
		require.EqualValues(t, numberOfEpochsToTest, counterValuesSeen[c],
			"counter value %d expected %d times, but received %d times",
			c, numberOfEpochsToTest, counterValuesSeen[c])
	}
	require.Len(t, counterValuesSeen, int(numberOfEligibleBallots))
}

func TestOracle_OwnATXNotFound(t *testing.T) {
	avgLayerSize := uint32(10)
	layersPerEpoch := uint32(20)
	o := createTestOracle(t, avgLayerSize, layersPerEpoch)
	lid := types.LayerID(layersPerEpoch * 3)
	ee, err := o.GetProposalEligibility(lid, types.RandomBeacon(), types.VRFPostIndex(1))
	require.ErrorIs(t, err, errMinerHasNoATXInPreviousEpoch)
	require.Nil(t, ee)
}
