package miner

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/system/mocks"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

const (
	defaultNumUnits = 1024
	activeSetSize   = 10
	networkDelay    = time.Second
)

type testOracle struct {
	*Oracle
	edSigner  *signing.EdSigner
	vrfSigner *signing.VRFSigner
	mClock    *MocklayerClock
	mSync     *mocks.MockSyncStateProvider
}

func genSigners(tb testing.TB) (*signing.EdSigner, *signing.VRFSigner) {
	tb.Helper()

	edSigner, err := signing.NewEdSigner()
	require.NoError(tb, err)
	vrfSigner, err := edSigner.VRFSigner()
	require.NoError(tb, err)
	return edSigner, vrfSigner
}

func genMinerATX(tb testing.TB, cdb *datastore.CachedDB, id types.ATXID, publishLayer types.LayerID, signer *signing.EdSigner, received time.Time) *types.VerifiedActivationTx {
	atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
		NIPostChallenge: types.NIPostChallenge{
			PublishEpoch: publishLayer.GetEpoch(),
		},
		NumUnits: defaultNumUnits,
	}}
	atx.SetID(id)
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(received)
	atx.SmesherID = signer.NodeID()
	vAtx, err := atx.Verify(0, 1)
	require.NoError(tb, err)
	require.NoError(tb, atxs.Add(cdb, vAtx))
	return vAtx
}

func genMinerMalfeasance(tb testing.TB, db sql.Executor, nodeID types.NodeID, received time.Time) {
	proof := &types.MalfeasanceProof{
		Layer: types.LayerID(11),
		Proof: types.Proof{
			Type: types.MultipleBallots,
			Data: &types.BallotProof{
				Messages: [2]types.BallotProofMsg{
					{},
					{},
				},
			},
		},
	}
	encoded, err := codec.Encode(proof)
	require.NoError(tb, err)
	require.NoError(tb, identities.SetMalicious(db, nodeID, encoded, received))
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
		EligibilityProofs: ee.Proofs[lid],
	}
	ballot.Signature = signer.Sign(signing.BALLOT, ballot.SignedBytes())
	ballot.SmesherID = signer.NodeID()
	require.NoError(tb, ballot.Initialize())
	return ballot
}

func createTestOracle(tb testing.TB, layerSize, layersPerEpoch uint32, minActiveSetWeight uint64) *testOracle {
	types.SetLayersPerEpoch(layersPerEpoch)

	lg := logtest.New(tb)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	edSigner, vrfSigner := genSigners(tb)
	cfg := config{
		layersPerEpoch:     layersPerEpoch,
		layerSize:          layerSize,
		minActiveSetWeight: minActiveSetWeight,
		networkDelay:       networkDelay,
		nodeID:             edSigner.NodeID(),
	}
	ctrl := gomock.NewController(tb)
	mClock := NewMocklayerClock(ctrl)
	mSync := mocks.NewMockSyncStateProvider(ctrl)
	return &testOracle{
		Oracle:    newMinerOracle(cfg, mClock, cdb, vrfSigner, mSync, lg),
		edSigner:  edSigner,
		vrfSigner: vrfSigner,
		mClock:    mClock,
		mSync:     mSync,
	}
}

type epochATXInfo struct {
	atxID     types.ATXID
	activeSet []types.ATXID
	beacon    types.Beacon
}

func genATXForTargetEpochs(tb testing.TB, cdb *datastore.CachedDB, start, end types.EpochID, signer *signing.EdSigner, layersPerEpoch uint32, received time.Time) map[types.EpochID]epochATXInfo {
	epochInfo := make(map[types.EpochID]epochATXInfo)
	for epoch := start; epoch < end; epoch++ {
		publishLayer := epoch.FirstLayer().Sub(layersPerEpoch)
		activeSet := types.RandomActiveSet(activeSetSize)
		atx := genMinerATX(tb, cdb, activeSet[0], publishLayer, signer, received)
		require.Equal(tb, atx.ID(), activeSet[0])
		info := epochATXInfo{
			beacon:    types.RandomBeacon(),
			activeSet: activeSet,
			atxID:     atx.ID(),
		}
		for _, id := range activeSet[1:] {
			signer, err := signing.NewEdSigner()
			require.NoError(tb, err)
			genMinerATX(tb, cdb, id, publishLayer, signer, received)
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

func testMinerOracleAndProposalValidator(t *testing.T, layerSize, layersPerEpoch uint32) {
	o := createTestOracle(t, layerSize, layersPerEpoch, 0)

	ctrl := gomock.NewController(t)
	mbc := mocks.NewMockBeaconCollector(ctrl)
	vrfVerifier := proposals.NewMockvrfVerifier(ctrl)
	vrfVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	tmock := proposals.NewMocktortoiseProvider(ctrl)
	tmock.EXPECT().GetBallot(gomock.Any()).AnyTimes().DoAndReturn(func(id types.BallotID) *tortoise.BallotData {
		ballot, err := ballots.Get(o.cdb, id)
		require.NoError(t, err)
		return &tortoise.BallotData{
			ID:           ballot.ID(),
			Layer:        ballot.Layer,
			ATXID:        ballot.AtxID,
			Smesher:      ballot.SmesherID,
			Beacon:       ballot.EpochData.Beacon,
			Eligiblities: ballot.EpochData.EligibilityCount,
		}
	})

	nonceFetcher := proposals.NewMocknonceFetcher(ctrl)
	nonce := types.VRFPostIndex(rand.Uint64())

	validator := proposals.NewEligibilityValidator(layerSize, layersPerEpoch, 0, o.mClock, tmock, o.cdb, mbc, o.log.WithName("blkElgValidator"), vrfVerifier,
		proposals.WithNonceFetcher(nonceFetcher),
	)

	startEpoch, numberOfEpochsToTest := uint32(2), uint32(2)
	startLayer := layersPerEpoch * startEpoch
	endLayer := types.LayerID(numberOfEpochsToTest * layersPerEpoch).Add(startLayer)
	counterValuesSeen := map[uint32]int{}
	epochStart := time.Now()
	o.mClock.EXPECT().LayerToTime(gomock.Any()).Return(epochStart).AnyTimes()
	received := epochStart.Add(-5 * networkDelay)
	epochInfo := genATXForTargetEpochs(t, o.cdb, types.EpochID(startEpoch), types.EpochID(startEpoch+numberOfEpochsToTest), o.edSigner, layersPerEpoch, received)
	for layer := types.LayerID(startLayer); layer.Before(endLayer); layer = layer.Add(1) {
		info, ok := epochInfo[layer.GetEpoch()]
		require.True(t, ok)
		ee, err := o.ProposalEligibility(layer, info.beacon, nonce)
		require.NoError(t, err)
		activesets.Add(o.cdb, ee.ActiveSet.Hash(), &types.EpochActiveSet{Epoch: ee.Epoch, Set: ee.ActiveSet})

		for _, proof := range ee.Proofs[layer] {
			b := genBallotWithEligibility(t, o.edSigner, info.beacon, layer, ee)
			b.SmesherID = o.edSigner.NodeID()
			o.mClock.EXPECT().CurrentLayer().Return(layer)
			mbc.EXPECT().ReportBeaconFromBallot(layer.GetEpoch(), b, info.beacon, gomock.Any()).Times(1)
			nonceFetcher.EXPECT().VRFNonce(b.SmesherID, layer.GetEpoch()).Return(nonce, nil).Times(1)
			eligible, err := validator.CheckEligibility(context.Background(), b, ee.ActiveSet)
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
	o := createTestOracle(t, avgLayerSize, layersPerEpoch, 0)
	lid := types.LayerID(layersPerEpoch * 3)
	o.mClock.EXPECT().LayerToTime(lid).Return(time.Now())
	ee, err := o.ProposalEligibility(lid, types.RandomBeacon(), types.VRFPostIndex(1))
	require.ErrorIs(t, err, errMinerHasNoATXInPreviousEpoch)
	require.Nil(t, ee)
}

func TestOracle_EligibilityCached(t *testing.T) {
	avgLayerSize := uint32(10)
	layersPerEpoch := uint32(20)
	o := createTestOracle(t, avgLayerSize, layersPerEpoch, 0)
	lid := types.LayerID(layersPerEpoch * 3)
	received := time.Now().Add(-1 * time.Hour)
	epochInfo := genATXForTargetEpochs(t, o.cdb, lid.GetEpoch(), lid.GetEpoch()+1, o.edSigner, layersPerEpoch, received)
	info, ok := epochInfo[lid.GetEpoch()]
	require.True(t, ok)
	o.mClock.EXPECT().LayerToTime(lid).Return(received.Add(time.Hour)).AnyTimes()
	ee1, err := o.ProposalEligibility(lid, info.beacon, types.VRFPostIndex(1))
	require.NoError(t, err)
	require.NotNil(t, ee1)

	// even if we pass a random beacon in this time, the cached value is still the same
	ee2, err := o.ProposalEligibility(lid, types.RandomBeacon(), types.VRFPostIndex(1))
	require.NoError(t, err)
	require.Equal(t, ee1, ee2)
}

func TestOracle_MinimalActiveSetWeight(t *testing.T) {
	avgLayerSize := uint32(10)
	layersPerEpoch := uint32(20)

	o := createTestOracle(t, avgLayerSize, layersPerEpoch, 0)
	lid := types.LayerID(layersPerEpoch * 3)
	received := time.Now().Add(-1 * time.Hour)
	epochInfo := genATXForTargetEpochs(t, o.cdb, lid.GetEpoch(), lid.GetEpoch()+1, o.edSigner, layersPerEpoch, received)

	info, ok := epochInfo[lid.GetEpoch()]
	require.True(t, ok)

	o.mClock.EXPECT().LayerToTime(lid).Return(received.Add(time.Hour)).AnyTimes()
	ee1, err := o.ProposalEligibility(lid, info.beacon, types.VRFPostIndex(1))
	require.NoError(t, err)
	require.NotNil(t, ee1)

	o.cfg.minActiveSetWeight = 100000
	o.cache.Epoch = 0
	ee2, err := o.ProposalEligibility(lid, info.beacon, types.VRFPostIndex(1))
	require.NoError(t, err)
	require.NotNil(t, ee1)

	require.Less(t, ee2.Slots, ee1.Slots)
}

func TestOracle_ATXGrade(t *testing.T) {
	for _, tc := range []struct {
		desc         string
		ownMalicious bool
	}{
		{
			desc: "own atx is good",
		},
		{
			desc:         "own atx is malicious",
			ownMalicious: true,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			avgLayerSize := uint32(50)
			layersPerEpoch := uint32(10)
			o := createTestOracle(t, avgLayerSize, layersPerEpoch, 0)
			lid := types.LayerID(layersPerEpoch * 3)
			epochStart := time.Now()
			o.mClock.EXPECT().LayerToTime(lid).Return(epochStart)

			goodTime := epochStart.Add(-4*networkDelay - time.Nanosecond)
			okTime := epochStart.Add(-3*networkDelay - time.Nanosecond)
			evilTime := epochStart.Add(-3 * networkDelay)
			publishLayer := (lid.GetEpoch() - 1).FirstLayer()
			received := goodTime
			if tc.ownMalicious {
				received = evilTime
			}
			ownAtx := genMinerATX(t, o.cdb, types.RandomATXID(), publishLayer, o.edSigner, received)
			expected := []types.ATXID{ownAtx.ID()}
			for i := 1; i < activeSetSize; i++ {
				sig, err := signing.NewEdSigner()
				require.NoError(t, err)
				atx := genMinerATX(t, o.cdb, types.RandomATXID(), publishLayer, sig, goodTime)
				expected = append(expected, atx.ID())
			}
			// add some atx that have good timing, with malicious proof arriving before epoch start
			for i := 0; i < activeSetSize; i++ {
				sig, err := signing.NewEdSigner()
				require.NoError(t, err)
				atx := genMinerATX(t, o.cdb, types.RandomATXID(), publishLayer, sig, goodTime)
				genMinerMalfeasance(t, o.cdb, sig.NodeID(), epochStart)
				expected = append(expected, atx.ID())
			}
			// add some atx that have good timing, with malfeasance proof arriving after epoch start
			for i := 0; i < activeSetSize; i++ {
				sig, err := signing.NewEdSigner()
				require.NoError(t, err)
				genMinerATX(t, o.cdb, types.RandomATXID(), publishLayer, sig, goodTime)
				genMinerMalfeasance(t, o.cdb, sig.NodeID(), epochStart.Add(-1*time.Nanosecond))
			}
			// add some atx that are acceptable
			for i := 0; i < activeSetSize; i++ {
				sig, err := signing.NewEdSigner()
				require.NoError(t, err)
				genMinerATX(t, o.cdb, types.RandomATXID(), publishLayer, sig, okTime)
			}
			// add some atx that are evil
			for i := 0; i < activeSetSize; i++ {
				sig, err := signing.NewEdSigner()
				require.NoError(t, err)
				genMinerATX(t, o.cdb, types.RandomATXID(), publishLayer, sig, evilTime)
			}
			ee, err := o.ProposalEligibility(lid, types.RandomBeacon(), types.VRFPostIndex(1))
			require.NoError(t, err)
			require.Equal(t, ownAtx.ID(), ee.Atx)
			require.ElementsMatch(t, expected, ee.ActiveSet)
			require.NotEmpty(t, ee.Proofs)
		})
	}
}

func createBallots(tb testing.TB, cdb *datastore.CachedDB, lid types.LayerID, numBallots int, common []types.ATXID) []*types.Ballot {
	var result []*types.Ballot
	for i := 0; i < numBallots; i++ {
		b := types.RandomBallot()
		b.Layer = lid
		b.AtxID = types.RandomATXID()
		b.RefBallot = types.EmptyBallotID
		b.EpochData = &types.EpochData{
			ActiveSetHash: types.RandomHash(),
		}
		b.Signature = types.RandomEdSignature()
		b.SmesherID = types.RandomNodeID()
		actives := append(common, b.AtxID)
		require.NoError(tb, activesets.Add(cdb, b.EpochData.ActiveSetHash, &types.EpochActiveSet{
			Epoch: lid.GetEpoch(),
			Set:   actives,
		}))
		require.NoError(tb, b.Initialize())
		require.NoError(tb, ballots.Add(cdb, b))
		result = append(result, b)
	}
	return result
}

func TestOracle_NewNode(t *testing.T) {
	for _, tc := range []struct {
		desc          string
		ownAtxInBlock bool
	}{
		{
			desc: "own atx not in block",
		},
		{
			desc:          "own atx in block",
			ownAtxInBlock: true,
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			avgLayerSize := uint32(10)
			lyrsPerEpoch := uint32(20)
			o := createTestOracle(t, avgLayerSize, lyrsPerEpoch, 0)
			o.cfg.goodAtxPct = 90
			lid := types.LayerID(lyrsPerEpoch * 3)
			o.mClock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now())
			common := types.RandomActiveSet(100)
			blts := createBallots(t, o.cdb, lid, 20, common)
			expected := common
			block := &types.Block{
				InnerBlock: types.InnerBlock{
					LayerIndex: lid.GetEpoch().FirstLayer(),
				},
			}
			for _, b := range blts {
				block.Rewards = append(block.Rewards, types.AnyReward{AtxID: b.AtxID})
				expected = append(expected, b.AtxID)
			}
			block.Initialize()
			require.NoError(t, blocks.Add(o.cdb, block))
			require.NoError(t, layers.SetApplied(o.cdb, lid, block.ID()))

			epoch := types.EpochID(2)
			var ownAtx types.ATXID
			var remaining []types.ATXID
			if tc.ownAtxInBlock {
				ownAtx = expected[0]
				remaining = expected[1:]
			} else {
				ownAtx = types.RandomATXID()
				remaining = expected
			}
			genMinerATX(t, o.cdb, ownAtx, epoch.FirstLayer(), o.edSigner, time.Now())
			for _, id := range remaining {
				signer, err := signing.NewEdSigner()
				require.NoError(t, err)
				genMinerATX(t, o.cdb, id, epoch.FirstLayer(), signer, time.Now())
			}
			if !tc.ownAtxInBlock {
				expected = append(expected, ownAtx)
			}

			ee, err := o.ProposalEligibility(lid, types.RandomBeacon(), types.VRFPostIndex(1))
			require.NoError(t, err)
			require.NotNil(t, ee)
			require.ElementsMatch(t, ee.ActiveSet, expected)
			require.Equal(t, ownAtx, ee.Atx)
		})
	}
}

func TestRefBallot(t *testing.T) {
	avgLayerSize := uint32(10)
	lyrsPerEpoch := uint32(20)
	o := createTestOracle(t, avgLayerSize, lyrsPerEpoch, 0)

	layer := types.LayerID(100)

	atx := types.ActivationTx{}
	atx.SmesherID = o.edSigner.NodeID()
	atx.PublishEpoch = layer.GetEpoch() - 1
	atx.SetID(types.ATXID{1})
	atx.SetEffectiveNumUnits(10)
	atx.NumUnits = 10
	atx.SetReceived(time.Now().Add(-100 * time.Second))
	vatx, err := atx.Verify(0, 100)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(o.cdb, vatx))

	ballot := types.Ballot{}
	ballot.Layer = layer
	ballot.AtxID = atx.ID()
	ballot.SmesherID = o.edSigner.NodeID()
	actives := types.ATXIDList{ballot.AtxID}
	ballot.EpochData = &types.EpochData{EligibilityCount: 1, ActiveSetHash: actives.Hash()}
	ballot.SetID(types.BallotID{1})
	require.NoError(t, ballots.Add(o.cdb, &ballot))
	activesets.Add(o.cdb, ballot.EpochData.ActiveSetHash, &types.EpochActiveSet{
		Epoch: layer.GetEpoch(),
		Set:   actives,
	})

	genATXForTargetEpochs(t, o.cdb, layer.GetEpoch(), layer.GetEpoch()+1, o.edSigner, layersPerEpoch, time.Now().Add(-1*time.Hour))
	ee, err := o.calcEligibilityProofs(layer, layer.GetEpoch(), types.Beacon{}, types.VRFPostIndex(101))
	require.NoError(t, err)
	require.NotEmpty(t, ee)
	require.Equal(t, 1, int(ee.Slots))
	require.Equal(t, atx.ID(), ee.Atx)
	require.ElementsMatch(t, actives, ee.ActiveSet)
}
