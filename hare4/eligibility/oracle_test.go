package eligibility

import (
	"context"
	"encoding/hex"
	"errors"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/spacemeshos/fixed"
	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/system/mocks"
)

const (
	defLayersPerEpoch uint32 = 10
	confidenceParam   uint32 = 3
	ballotsPerLayer          = 50
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(defLayersPerEpoch)
	res := m.Run()
	os.Exit(res)
}

type testOracle struct {
	*Oracle
	tb        testing.TB
	db        sql.StateDatabase
	atxsdata  *atxsdata.Data
	mBeacon   *mocks.MockBeaconGetter
	mVerifier *MockvrfVerifier
}

func defaultOracle(tb testing.TB) *testOracle {
	db := statesql.InMemoryTest(tb)
	atxsdata := atxsdata.New()

	ctrl := gomock.NewController(tb)
	mBeacon := mocks.NewMockBeaconGetter(ctrl)
	mVerifier := NewMockvrfVerifier(ctrl)

	to := &testOracle{
		Oracle: New(
			mBeacon,
			db,
			atxsdata,
			mVerifier,
			defLayersPerEpoch,
			WithConfig(Config{ConfidenceParam: confidenceParam}),
			WithLogger(zaptest.NewLogger(tb)),
		),
		tb:        tb,
		mBeacon:   mBeacon,
		mVerifier: mVerifier,
		db:        db,
		atxsdata:  atxsdata,
	}
	return to
}

func (t *testOracle) createBallots(
	lid types.LayerID,
	activeSet types.ATXIDList,
	miners []types.NodeID,
) []*types.Ballot {
	t.tb.Helper()
	numBallots := ballotsPerLayer
	if len(activeSet) < numBallots {
		numBallots = len(activeSet)
	}
	var result []*types.Ballot
	for i := 0; i < numBallots; i++ {
		b := types.RandomBallot()
		b.Layer = lid
		b.AtxID = activeSet[i]
		b.RefBallot = types.EmptyBallotID
		b.EpochData = &types.EpochData{ActiveSetHash: activeSet.Hash()}
		b.Signature = types.RandomEdSignature()
		b.SmesherID = miners[i]
		require.NoError(t.tb, b.Initialize())
		require.NoError(t.tb, ballots.Add(t.db, b))
		activesets.Add(t.db, b.EpochData.ActiveSetHash, &types.EpochActiveSet{
			Epoch: lid.GetEpoch(),
			Set:   activeSet,
		})
		result = append(result, b)
	}
	return result
}

func (t *testOracle) createBlock(blts []*types.Ballot) {
	t.tb.Helper()
	block := &types.Block{
		InnerBlock: types.InnerBlock{
			LayerIndex: blts[0].Layer,
		},
	}
	for _, b := range blts {
		block.Rewards = append(block.Rewards, types.AnyReward{AtxID: b.AtxID})
	}
	block.Initialize()
	require.NoError(t.tb, blocks.Add(t.db, block))
	require.NoError(t.tb, layers.SetApplied(t.db, block.LayerIndex, block.ID()))
}

func (t *testOracle) createLayerData(lid types.LayerID, numMiners int) []types.NodeID {
	t.tb.Helper()
	activeSet := types.RandomActiveSet(numMiners)
	miners := t.createActiveSet(lid.GetEpoch().FirstLayer().Sub(1), activeSet)
	blts := t.createBallots(lid, activeSet, miners)
	t.createBlock(blts)
	return miners
}

func (t *testOracle) createActiveSet(
	lid types.LayerID,
	activeSet []types.ATXID,
) []types.NodeID {
	var miners []types.NodeID
	for i, id := range activeSet {
		nodeID := types.BytesToNodeID([]byte(strconv.Itoa(i)))
		miners = append(miners, nodeID)
		atx := &types.ActivationTx{
			PublishEpoch: lid.GetEpoch(),
			Weight:       uint64(i + 1),
			SmesherID:    nodeID,
		}
		atx.SetID(id)
		atx.SetReceived(time.Now())
		t.addAtx(atx)
	}
	return miners
}

func (t *testOracle) addAtx(atx *types.ActivationTx) {
	t.tb.Helper()
	require.NoError(t.tb, atxs.Add(t.db, atx, types.AtxBlob{}))
	t.atxsdata.AddFromAtx(atx, false)
}

// create n identities with weights and identifiers 1,2,3,...,n.
func createIdentities(n int) map[types.NodeID]identityWeight {
	m := map[types.NodeID]identityWeight{}
	for i := 0; i < n; i++ {
		m[types.BytesToNodeID([]byte(strconv.Itoa(i)))] = identityWeight{
			atx:    types.ATXID(types.BytesToHash([]byte(strconv.Itoa(i)))),
			weight: uint64(i + 1),
		}
	}
	return m
}

func TestCalcEligibility(t *testing.T) {
	nid := types.NodeID{1, 1}

	t.Run("zero committee", func(t *testing.T) {
		o := defaultOracle(t)
		res, err := o.CalcEligibility(context.Background(), types.LayerID(50), 1, 0, nid, types.EmptyVrfSignature)
		require.ErrorIs(t, err, errZeroCommitteeSize)
		require.Equal(t, 0, int(res))
	})

	t.Run("empty active set", func(t *testing.T) {
		o := defaultOracle(t)
		o.mBeacon.EXPECT().GetBeacon(gomock.Any())
		lid := types.EpochID(5).FirstLayer()
		res, err := o.CalcEligibility(context.Background(), lid, 1, 1, nid, types.EmptyVrfSignature)
		require.ErrorIs(t, err, errEmptyActiveSet)
		require.Equal(t, 0, int(res))
	})

	t.Run("miner not active", func(t *testing.T) {
		o := defaultOracle(t)
		lid := types.EpochID(5).FirstLayer()
		o.createLayerData(lid.Sub(defLayersPerEpoch), 11)
		res, err := o.CalcEligibility(context.Background(), lid, 1, 1, nid, types.EmptyVrfSignature)
		require.ErrorIs(t, err, ErrNotActive)
		require.Equal(t, 0, int(res))
	})

	t.Run("beacon failure", func(t *testing.T) {
		o := defaultOracle(t)
		layer := types.EpochID(5).FirstLayer()
		miners := o.createLayerData(layer.Sub(defLayersPerEpoch), 5)
		errUnknown := errors.New("unknown")
		o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(types.EmptyBeacon, errUnknown).Times(1)

		res, err := o.CalcEligibility(context.Background(), layer, 0, 1, miners[0], types.EmptyVrfSignature)
		require.ErrorIs(t, err, errUnknown)
		require.Equal(t, 0, int(res))
	})

	t.Run("verify failure", func(t *testing.T) {
		o := defaultOracle(t)
		layer := types.EpochID(5).FirstLayer()
		miners := o.createLayerData(layer.Sub(defLayersPerEpoch), 5)
		o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(types.RandomBeacon(), nil).Times(1)
		o.mVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(false).Times(1)

		res, err := o.CalcEligibility(context.Background(), layer, 0, 1, miners[0], types.EmptyVrfSignature)
		require.NoError(t, err)
		require.Equal(t, 0, int(res))
	})

	t.Run("empty active with fallback", func(t *testing.T) {
		o := defaultOracle(t)
		o.mBeacon.EXPECT().GetBeacon(gomock.Any())
		lid := types.EpochID(5).FirstLayer().Add(o.cfg.ConfidenceParam)
		res, err := o.CalcEligibility(context.Background(), lid, 1, 1, nid, types.EmptyVrfSignature)
		require.ErrorIs(t, err, errEmptyActiveSet)
		require.Equal(t, 0, int(res))

		activeSet := types.RandomActiveSet(111)
		miners := o.createActiveSet(types.EpochID(4).FirstLayer(), activeSet)
		o.UpdateActiveSet(5, activeSet)
		o.mBeacon.EXPECT().GetBeacon(lid.GetEpoch()).Return(types.RandomBeacon(), nil)
		o.mVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true)
		_, err = o.CalcEligibility(context.Background(), lid, 1, 1, miners[0], types.EmptyVrfSignature)
		require.NoError(t, err)
	})

	t.Run("miner active", func(t *testing.T) {
		o := defaultOracle(t)
		lid := types.EpochID(5).FirstLayer()
		beacon := types.Beacon{1, 0, 0, 0}
		miners := o.createLayerData(lid.Sub(defLayersPerEpoch), 5)
		sigs := map[string]uint16{
			"0516a574aef37257d6811ea53ef55d4cbb0e14674900a0d5165bd6742513840d" +
				"02442d979fdabc7059645d1e8f8a0f44d0db2aa90f23374dd74a3636d4ecdab7": 1,
			"73929b4b69090bb6133e2f8cd73989b35228e7e6d8c6745e4100d9c5eb48ca26" +
				"24ee2889e55124195a130f74ea56e53a73a1c4dee60baa13ad3b1c0ed4f80d9c": 0,
			"e2c27ad65b752b763173b588518764b6c1e42896d57e0eabef9bcac68e07b877" +
				"29a4ef9e5f17d8c1cb34ffd0d65ee9a7e63e63b77a7bcab1140a76fc04c271de": 0,
			"384460966938c87644987fe00c0f9d4f9a5e2dcd4bdc08392ed94203895ba325" +
				"036725a22346e35aa707993babef716aa1b6b3dfc653a44cb23ac8f743cbbc3d": 1,
			"15c5f565a75888970059b070bfaed1998a9d423ddac9f6af83da51db02149044" +
				"ea6aeb86294341c7a950ac5de2855bbebc11cc28b02c08bc903e4cf41439717d": 1,
		}
		for vrf, exp := range sigs {
			sig, err := hex.DecodeString(vrf)
			require.NoError(t, err)

			var vrfSig types.VrfSignature
			copy(vrfSig[:], sig)

			o.mBeacon.EXPECT().GetBeacon(lid.GetEpoch()).Return(beacon, nil).Times(1)
			o.mVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).Times(1)
			res, err := o.CalcEligibility(context.Background(), lid, 1, 10, miners[0], vrfSig)
			require.NoError(t, err, vrf)
			require.Equal(t, exp, res, vrf)
		}
	})
}

func TestCalcEligibilityWithSpaceUnit(t *testing.T) {
	const committeeSize = 800
	tcs := []struct {
		desc      string
		numMiners int
	}{
		{
			desc:      "small network",
			numMiners: 50,
		},
		{
			desc:      "large network",
			numMiners: 2000,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			o := defaultOracle(t)
			o.mVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).AnyTimes()

			lid := types.EpochID(5).FirstLayer()
			beacon := types.Beacon{1, 0, 0, 0}
			miners := o.createLayerData(lid.Sub(defLayersPerEpoch), tc.numMiners)

			var eligibilityCount uint16
			for _, nodeID := range miners {
				sig := types.RandomVrfSignature()

				o.mBeacon.EXPECT().GetBeacon(lid.GetEpoch()).Return(beacon, nil).Times(2)
				res, err := o.CalcEligibility(context.Background(), lid, 1, committeeSize, nodeID, sig)
				require.NoError(t, err)

				valid, err := o.Validate(context.Background(), lid, 1, committeeSize, nodeID, sig, res)
				require.NoError(t, err)
				require.True(t, valid)

				eligibilityCount += res
			}

			require.InDelta(t, committeeSize, eligibilityCount, committeeSize*15/100) // up to 15% difference
			// a correct check would be to calculate the expected variance of the binomial distribution
			// which depends on the number of miners and the number of units each miner has
			// and then assert that the difference is within 3 standard deviations of the expected value
		})
	}
}

func BenchmarkOracle_CalcEligibility(b *testing.B) {
	r := require.New(b)

	o := defaultOracle(b)
	o.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil).AnyTimes()
	o.mVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	numOfMiners := 2000
	committeeSize := 800

	lid := types.EpochID(5).FirstLayer()
	o.createLayerData(lid, numOfMiners)

	var nodeIDs []types.NodeID
	for pubkey := range createIdentities(b.N) {
		nodeIDs = append(nodeIDs, pubkey)
	}
	b.ResetTimer()
	for _, nodeID := range nodeIDs {
		res, err := o.CalcEligibility(context.Background(), lid, 1, committeeSize, nodeID, types.EmptyVrfSignature)

		if err == nil {
			valid, err := o.Validate(context.Background(), lid, 1, committeeSize, nodeID, types.EmptyVrfSignature, res)
			r.NoError(err)
			r.True(valid)
		}
	}
}

func Test_VrfSignVerify(t *testing.T) {
	// eligibility of the proof depends on the identity
	rng := rand.New(rand.NewSource(5))

	signer, err := signing.NewEdSigner(signing.WithKeyFromRand(rng))
	require.NoError(t, err)

	o := defaultOracle(t)
	nid := signer.NodeID()

	lid := types.EpochID(5).FirstLayer().Add(confidenceParam)
	first := types.EpochID(5).FirstLayer()
	prevEpoch := lid.GetEpoch() - 1
	o.mBeacon.EXPECT().GetBeacon(lid.GetEpoch()).Return(types.Beacon{1, 0, 0, 0}, nil).AnyTimes()

	numMiners := 2
	activeSet := types.RandomActiveSet(numMiners)
	atx1 := &types.ActivationTx{
		PublishEpoch: prevEpoch,
		Weight:       1 * 1024,
		SmesherID:    signer.NodeID(),
	}
	atx1.SetID(activeSet[0])
	atx1.SetReceived(time.Now())
	o.addAtx(atx1)

	signer2, err := signing.NewEdSigner(signing.WithKeyFromRand(rng))
	require.NoError(t, err)

	atx2 := &types.ActivationTx{
		PublishEpoch: prevEpoch,
		Weight:       9 * 1024,
		SmesherID:    signer2.NodeID(),
	}
	atx2.SetID(activeSet[1])
	atx2.SetReceived(time.Now())
	o.addAtx(atx2)
	miners := []types.NodeID{atx1.SmesherID, atx2.SmesherID}
	o.createBlock(o.createBallots(first, activeSet, miners))

	o.vrfVerifier = signing.NewVRFVerifier()

	// round is handpicked for vrf signature to pass
	const round = 0

	proof, err := o.Proof(context.Background(), signer.VRFSigner(), lid, round)
	require.NoError(t, err)

	res, err := o.CalcEligibility(context.Background(), lid, round, 10, nid, proof)
	require.NoError(t, err)
	require.Equal(t, 1, int(res))

	valid, err := o.Validate(context.Background(), lid, round, 10, nid, proof, 1)
	require.NoError(t, err)
	require.True(t, valid)
}

func Test_Proof_BeaconError(t *testing.T) {
	o := defaultOracle(t)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	layer := types.LayerID(2)
	errUnknown := errors.New("unknown")
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(types.EmptyBeacon, errUnknown).Times(1)

	_, err = o.Proof(context.Background(), signer.VRFSigner(), layer, 3)
	require.ErrorIs(t, err, errUnknown)
}

func Test_Proof(t *testing.T) {
	o := defaultOracle(t)
	layer := types.LayerID(2)
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(types.Beacon{1, 0, 0, 0}, nil)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	sig, err := o.Proof(context.Background(), signer.VRFSigner(), layer, 3)
	require.NoError(t, err)
	require.NotNil(t, sig)
}

func TestOracle_IsIdentityActive(t *testing.T) {
	o := defaultOracle(t)
	layer := types.LayerID(defLayersPerEpoch * 4)
	numMiners := 2
	miners := o.createLayerData(layer.Sub(defLayersPerEpoch), numMiners)
	for _, nodeID := range miners {
		v, err := o.IsIdentityActiveOnConsensusView(context.Background(), nodeID, layer)
		require.NoError(t, err)
		require.True(t, v)
	}
	v, err := o.IsIdentityActiveOnConsensusView(context.Background(), types.NodeID{7, 7, 7}, layer)
	require.NoError(t, err)
	require.False(t, v)
}

func TestBuildVRFMessage_BeaconError(t *testing.T) {
	o := defaultOracle(t)
	errUnknown := errors.New("unknown")
	o.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.EmptyBeacon, errUnknown).Times(1)
	msg, err := o.buildVRFMessage(context.Background(), types.LayerID(1), 1)
	require.ErrorIs(t, err, errUnknown)
	require.Nil(t, msg)
}

func TestBuildVRFMessage(t *testing.T) {
	o := defaultOracle(t)
	firstLayer := types.LayerID(1)
	secondLayer := firstLayer.Add(1)
	beacon := types.RandomBeacon()
	o.mBeacon.EXPECT().GetBeacon(firstLayer.GetEpoch()).Return(beacon, nil).Times(1)
	m1, err := o.buildVRFMessage(context.Background(), firstLayer, 2)
	require.NoError(t, err)

	// check not same for different round
	o.mBeacon.EXPECT().GetBeacon(firstLayer.GetEpoch()).Return(beacon, nil).Times(1)
	m3, err := o.buildVRFMessage(context.Background(), firstLayer, 3)
	require.NoError(t, err)
	require.NotEqual(t, m1, m3)

	// check not same for different layer
	o.mBeacon.EXPECT().GetBeacon(firstLayer.GetEpoch()).Return(beacon, nil).Times(1)
	m4, err := o.buildVRFMessage(context.Background(), secondLayer, 2)
	require.NoError(t, err)
	require.NotEqual(t, m1, m4)

	// check same call returns same result
	o.mBeacon.EXPECT().GetBeacon(firstLayer.GetEpoch()).Return(beacon, nil).Times(1)
	m5, err := o.buildVRFMessage(context.Background(), firstLayer, 2)
	require.NoError(t, err)
	require.Equal(t, m1, m5) // check same result
}

func TestBuildVRFMessage_Concurrency(t *testing.T) {
	o := defaultOracle(t)

	total := 1000
	expectAdd := 10
	wg := sync.WaitGroup{}
	firstLayer := types.LayerID(1)
	o.mBeacon.EXPECT().GetBeacon(firstLayer.GetEpoch()).Return(types.RandomBeacon(), nil).AnyTimes()
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(x int) {
			_, err := o.buildVRFMessage(context.Background(), firstLayer, uint32(x%expectAdd))
			assert.NoError(t, err)
			wg.Done()
		}(i)
	}

	wg.Wait()
}

func TestActiveSet(t *testing.T) {
	numMiners := 5
	o := defaultOracle(t)
	targetEpoch := types.EpochID(5)
	layer := targetEpoch.FirstLayer().Add(o.cfg.ConfidenceParam)
	o.createLayerData(targetEpoch.FirstLayer(), numMiners)

	aset, err := o.actives(context.Background(), layer)
	require.NoError(t, err)
	require.ElementsMatch(
		t,
		maps.Keys(createIdentities(numMiners)),
		maps.Keys(aset.set),
		"assertion relies on the enumeration of identities",
	)

	got, err := o.ActiveSet(context.Background(), targetEpoch)
	require.NoError(t, err)
	require.Len(t, got, len(aset.set))
	for _, id := range got {
		atx, err := atxs.Get(o.db, id)
		require.NoError(t, err)
		require.Contains(t, aset.set, atx.SmesherID, "id %s atx %s", id.ShortString(), atx.ShortString())
		delete(aset.set, atx.SmesherID)
	}
}

func TestActives(t *testing.T) {
	numMiners := 5
	t.Run("genesis bootstrap", func(t *testing.T) {
		o := defaultOracle(t)
		first := types.GetEffectiveGenesis().Add(1)
		bootstrap := types.RandomActiveSet(numMiners)
		o.createActiveSet(types.EpochID(1).FirstLayer(), bootstrap)
		o.UpdateActiveSet(types.GetEffectiveGenesis().GetEpoch()+1, bootstrap)

		for lid := types.LayerID(0); lid.Before(first); lid = lid.Add(1) {
			activeSet, err := o.actives(context.Background(), lid)
			require.ErrorIs(t, err, errEmptyActiveSet)
			require.Nil(t, activeSet)
		}
		activeSet, err := o.actives(context.Background(), first)
		require.NoError(t, err)
		require.ElementsMatch(
			t,
			maps.Keys(createIdentities(numMiners)),
			maps.Keys(activeSet.set),
			"assertion relies on the enumeration of identities",
		)
	})
	t.Run("steady state", func(t *testing.T) {
		numMiners++
		o := defaultOracle(t)
		o.mBeacon.EXPECT().GetBeacon(gomock.Any())
		layer := types.EpochID(4).FirstLayer()
		o.createLayerData(layer, numMiners)

		start := layer.Add(o.cfg.ConfidenceParam)
		activeSet, err := o.actives(context.Background(), start)
		require.NoError(t, err)
		require.ElementsMatch(
			t,
			maps.Keys(createIdentities(numMiners)),
			maps.Keys(activeSet.set),
			"assertion relies on the enumeration of identities",
		)
		end := (layer.GetEpoch() + 1).FirstLayer().Add(o.cfg.ConfidenceParam)

		for lid := start.Add(1); lid.Before(end); lid = lid.Add(1) {
			got, err := o.actives(context.Background(), lid)
			require.NoError(t, err)
			// cached
			require.Equal(t, &activeSet, &got)
		}
		got, err := o.actives(context.Background(), end)
		require.ErrorIs(t, err, errEmptyActiveSet)
		require.Nil(t, got)
	})
	t.Run("use fallback despite block", func(t *testing.T) {
		numMiners++
		o := defaultOracle(t)
		o.mBeacon.EXPECT().GetBeacon(gomock.Any()).AnyTimes()
		layer := types.EpochID(4).FirstLayer()
		end := layer.Add(o.cfg.ConfidenceParam)
		o.createLayerData(layer, numMiners)
		fallback := types.RandomActiveSet(numMiners + 1)
		o.createActiveSet(types.EpochID(3).FirstLayer(), fallback)
		o.UpdateActiveSet(end.GetEpoch(), fallback)

		for lid := layer; lid.Before(end); lid = lid.Add(1) {
			got, err := o.actives(context.Background(), lid)
			require.ErrorIs(t, err, errEmptyActiveSet)
			require.Nil(t, got)
		}
		activeSet, err := o.actives(context.Background(), end)
		require.NoError(t, err)
		require.ElementsMatch(
			t,
			maps.Keys(createIdentities(numMiners+1)),
			maps.Keys(activeSet.set),
			"assertion relies on the enumeration of identities",
		)
	})
	t.Run("recover at epoch start", func(t *testing.T) {
		numMiners++
		o := defaultOracle(t)
		o.mBeacon.EXPECT().GetBeacon(gomock.Any()).AnyTimes()
		layer := types.EpochID(4).FirstLayer()
		old := types.GetEffectiveGenesis()
		types.SetEffectiveGenesis(layer.Uint32() - 1)
		t.Cleanup(func() {
			types.SetEffectiveGenesis(old.Uint32())
		})
		o.createLayerData(layer, numMiners)
		fallback := types.RandomActiveSet(numMiners + 1)
		o.createActiveSet(types.EpochID(3).FirstLayer(), fallback)
		o.UpdateActiveSet(layer.GetEpoch(), fallback)

		activeSet, err := o.actives(context.Background(), layer)
		require.NoError(t, err)
		require.ElementsMatch(
			t,
			maps.Keys(createIdentities(numMiners+1)),
			maps.Keys(activeSet.set),
			"assertion relies on the enumeration of identities",
		)
		activeSet2, err := o.actives(context.Background(), layer+1)
		require.NoError(t, err)
		require.Equal(t, activeSet, activeSet2)
	})
}

func TestActives_ConcurrentCalls(t *testing.T) {
	r := require.New(t)
	o := defaultOracle(t)
	layer := types.LayerID(100)
	o.createLayerData(layer.Sub(defLayersPerEpoch), 5)

	mc := NewMockactiveSetCache(gomock.NewController(t))
	firstCall := true
	mc.EXPECT().Get(layer.GetEpoch() - 1).DoAndReturn(
		func(types.EpochID) (*cachedActiveSet, bool) {
			if firstCall {
				firstCall = false
				return nil, false
			}
			aset := cachedActiveSet{set: createIdentities(5)}
			for _, value := range aset.set {
				aset.total += value.weight
			}
			return &aset, true
		}).Times(102)
	mc.EXPECT().Add(layer.GetEpoch()-1, gomock.Any())
	o.activesCache = mc

	var wg sync.WaitGroup
	wg.Add(102)
	runFn := func() {
		_, err := o.actives(context.Background(), layer)
		r.NoError(err)
		wg.Done()
	}

	// outstanding probability for concurrent access to calc active set size
	for i := 0; i < 100; i++ {
		go runFn()
	}

	// make sure we wait at least two calls duration
	runFn()
	runFn()
	wg.Wait()
}

func TestMaxSupportedN(t *testing.T) {
	n := maxSupportedN
	p := fixed.DivUint64(800, uint64(n*100))
	x := 0

	require.Panics(t, func() {
		fixed.BinCDF(n+1, p, x)
	})

	require.NotPanics(t, func() {
		for x = 0; x < 800; x++ {
			fixed.BinCDF(n, p, x)
		}
	})
}

func TestActiveSetMatrix(t *testing.T) {
	t.Parallel()

	target := types.EpochID(4)
	bgen := func(
		id types.BallotID,
		lid types.LayerID,
		node types.NodeID,
		beacon types.Beacon,
		atxs types.ATXIDList,
		option ...func(*types.Ballot),
	) types.Ballot {
		ballot := types.Ballot{}
		ballot.Layer = lid
		ballot.EpochData = &types.EpochData{Beacon: beacon, ActiveSetHash: atxs.Hash()}
		ballot.SmesherID = node
		ballot.SetID(id)
		for _, opt := range option {
			opt(&ballot)
		}
		return ballot
	}
	agen := func(
		id types.ATXID,
		node types.NodeID,
		option ...func(*types.ActivationTx),
	) *types.ActivationTx {
		atx := &types.ActivationTx{
			PublishEpoch: target - 1,
			SmesherID:    node,
			NumUnits:     1,
			TickCount:    1,
		}
		atx.SetID(id)
		atx.SetReceived(time.Time{}.Add(1))

		for _, opt := range option {
			opt(atx)
		}
		return atx
	}

	for _, tc := range []struct {
		desc    string
		beacon  types.Beacon // local beacon
		ballots []types.Ballot
		atxs    []*types.ActivationTx
		actives []types.ATXIDList
		expect  any
	}{
		{
			desc:   "merged activesets",
			beacon: types.Beacon{1},
			ballots: []types.Ballot{
				bgen(
					types.BallotID{1},
					target.FirstLayer(),
					types.NodeID{1},
					types.Beacon{1},
					[]types.ATXID{{1}, {2}},
				),
				bgen(
					types.BallotID{2},
					target.FirstLayer(),
					types.NodeID{2},
					types.Beacon{1},
					[]types.ATXID{{2}, {3}},
				),
			},
			atxs: []*types.ActivationTx{
				agen(types.ATXID{1}, types.NodeID{1}),
				agen(types.ATXID{2}, types.NodeID{2}),
				agen(types.ATXID{3}, types.NodeID{3}),
			},
			actives: []types.ATXIDList{{{1}, {2}}, {{2}, {3}}},
			expect:  []types.ATXID{{1}, {2}, {3}},
		},
		{
			desc:   "filter by beacon",
			beacon: types.Beacon{1},
			ballots: []types.Ballot{
				bgen(
					types.BallotID{1},
					target.FirstLayer(),
					types.NodeID{1},
					types.Beacon{1},
					[]types.ATXID{{1}, {2}},
				),
				bgen(
					types.BallotID{2},
					target.FirstLayer(),
					types.NodeID{2},
					types.Beacon{2, 2, 2, 2},
					[]types.ATXID{{2}, {3}},
				),
			},
			atxs: []*types.ActivationTx{
				agen(types.ATXID{1}, types.NodeID{1}),
				agen(types.ATXID{2}, types.NodeID{2}),
			},
			actives: []types.ATXIDList{{{1}, {2}}, {{2}, {3}}},
			expect:  []types.ATXID{{1}, {2}},
		},
		{
			desc:   "no local beacon",
			beacon: types.EmptyBeacon,
			ballots: []types.Ballot{
				bgen(
					types.BallotID{1},
					target.FirstLayer(),
					types.NodeID{1},
					types.Beacon{1},
					[]types.ATXID{{1}, {2}},
				),
				bgen(
					types.BallotID{2},
					target.FirstLayer(),
					types.NodeID{2},
					types.Beacon{2, 2, 2, 2},
					[]types.ATXID{{2}, {3}},
				),
			},
			atxs:    []*types.ActivationTx{},
			actives: []types.ATXIDList{{{1}, {2}}, {{2}, {3}}},
			expect:  "not found",
		},
		{
			desc:   "unknown atxs",
			beacon: types.Beacon{1},
			ballots: []types.Ballot{
				bgen(
					types.BallotID{1},
					target.FirstLayer(),
					types.NodeID{1},
					types.Beacon{1},
					[]types.ATXID{{1}, {2}},
				),
				bgen(
					types.BallotID{2},
					target.FirstLayer(),
					types.NodeID{2},
					types.Beacon{2, 2, 2, 2},
					[]types.ATXID{{2}, {3}},
				),
			},
			atxs:    []*types.ActivationTx{},
			actives: []types.ATXIDList{{{1}, {2}}, {{2}, {3}}},
			expect:  "missing atx in atxsdata",
		},
		{
			desc:   "ballot no epoch data",
			beacon: types.Beacon{1},
			ballots: []types.Ballot{
				bgen(
					types.BallotID{1},
					target.FirstLayer(),
					types.NodeID{1},
					types.Beacon{1},
					[]types.ATXID{{1}, {2}},
					func(ballot *types.Ballot) {
						ballot.EpochData = nil
					},
				),
				bgen(
					types.BallotID{2},
					target.FirstLayer(),
					types.NodeID{2},
					types.Beacon{1},
					[]types.ATXID{{2}, {3}},
				),
			},
			atxs: []*types.ActivationTx{
				agen(types.ATXID{2}, types.NodeID{2}),
				agen(types.ATXID{3}, types.NodeID{3}),
			},
			actives: []types.ATXIDList{{{2}, {3}}},
			expect:  []types.ATXID{{2}, {3}},
		},
		{
			desc:   "wrong target epoch",
			beacon: types.Beacon{1},
			ballots: []types.Ballot{
				bgen(
					types.BallotID{1},
					target.FirstLayer(),
					types.NodeID{1},
					types.Beacon{1},
					[]types.ATXID{{2}},
				),
			},
			atxs: []*types.ActivationTx{
				agen(types.ATXID{2}, types.NodeID{1}, func(verified *types.ActivationTx) {
					verified.PublishEpoch = target
				}),
			},
			actives: []types.ATXIDList{{{2}}},
			expect:  "missing atx in atxsdata 4/0200000000",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			oracle := defaultOracle(t)
			for _, actives := range tc.actives {
				require.NoError(t, activesets.Add(oracle.db, actives.Hash(), &types.EpochActiveSet{Set: actives}))
			}
			for _, ballot := range tc.ballots {
				require.NoError(t, ballots.Add(oracle.db, &ballot))
			}
			for _, atx := range tc.atxs {
				require.NoError(t, atxs.Add(oracle.db, atx, types.AtxBlob{}))
				oracle.atxsdata.AddFromAtx(atx, false)
			}
			if tc.beacon != types.EmptyBeacon {
				oracle.mBeacon.EXPECT().GetBeacon(target).Return(tc.beacon, nil)
			} else {
				oracle.mBeacon.EXPECT().GetBeacon(target).Return(types.EmptyBeacon, sql.ErrNotFound)
			}
			rst, err := oracle.ActiveSet(context.TODO(), target)

			switch typed := tc.expect.(type) {
			case []types.ATXID:
				require.NoError(t, err)
				require.ElementsMatch(t, typed, rst)
			case string:
				require.Empty(t, rst)
				require.ErrorContains(t, err, typed)
			default:
				require.Failf(t, "unknown assert type", "%v", typed)
			}
		})
	}
}

func TestResetCache(t *testing.T) {
	oracle := defaultOracle(t)
	ctrl := gomock.NewController(t)

	prev := oracle.activesCache
	prev.Add(1, nil)

	oracle.resetCacheOnSynced(context.Background())
	require.Equal(t, prev, oracle.activesCache)

	sync := mocks.NewMockSyncStateProvider(ctrl)
	oracle.SetSync(sync)

	sync.EXPECT().IsSynced(gomock.Any()).Return(false)
	oracle.resetCacheOnSynced(context.Background())
	require.Equal(t, prev, oracle.activesCache)

	sync.EXPECT().IsSynced(gomock.Any()).Return(true)
	oracle.resetCacheOnSynced(context.Background())
	require.NotEqual(t, prev, oracle.activesCache)

	prev = oracle.activesCache
	prev.Add(1, nil)

	sync.EXPECT().IsSynced(gomock.Any()).Return(true)
	oracle.resetCacheOnSynced(context.Background())
	require.Equal(t, prev, oracle.activesCache)
}

func FuzzVrfMessageConsistency(f *testing.F) {
	tester.FuzzConsistency[VrfMessage](f)
}

func FuzzVrfMessageSafety(f *testing.F) {
	tester.FuzzSafety[VrfMessage](f)
}
