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

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/fixed"
	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility/config"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
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
	mBeacon       *mocks.MockBeaconGetter
	mVerifier     *MockvrfVerifier
	mNonceFetcher *MocknonceFetcher
}

func defaultOracle(t testing.TB) *testOracle {
	lg := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)

	ctrl := gomock.NewController(t)
	mb := mocks.NewMockBeaconGetter(ctrl)
	verifier := NewMockvrfVerifier(ctrl)
	nonceFetcher := NewMocknonceFetcher(ctrl)

	to := &testOracle{
		Oracle: New(mb, cdb, verifier, nil, defLayersPerEpoch, config.Config{ConfidenceParam: confidenceParam}, lg,
			withNonceFetcher(nonceFetcher),
		),
		mBeacon:       mb,
		mVerifier:     verifier,
		mNonceFetcher: nonceFetcher,
	}
	return to
}

func createBallots(tb testing.TB, cdb *datastore.CachedDB, lid types.LayerID, activeSet []types.ATXID, miners []types.NodeID) []*types.Ballot {
	tb.Helper()
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
		b.EpochData = &types.EpochData{}
		b.ActiveSet = activeSet
		b.Signature = types.RandomEdSignature()
		b.SmesherID = miners[i]
		require.NoError(tb, b.Initialize())
		require.NoError(tb, ballots.Add(cdb, b))
		result = append(result, b)
	}
	return result
}

func createBlock(tb testing.TB, cdb *datastore.CachedDB, blts []*types.Ballot) {
	tb.Helper()
	block := &types.Block{
		InnerBlock: types.InnerBlock{
			LayerIndex: blts[0].Layer,
		},
	}
	for _, b := range blts {
		block.Rewards = append(block.Rewards, types.AnyReward{AtxID: b.AtxID})
	}
	block.Initialize()
	require.NoError(tb, blocks.Add(cdb, block))
	require.NoError(tb, certificates.Add(cdb, blts[0].Layer, &types.Certificate{BlockID: block.ID()}))
}

func createLayerData(tb testing.TB, cdb *datastore.CachedDB, lid types.LayerID, numMiners int) []types.NodeID {
	tb.Helper()
	activeSet := types.RandomActiveSet(numMiners)
	miners := createActiveSet(tb, cdb, lid.GetEpoch().FirstLayer().Sub(1), activeSet)
	blts := createBallots(tb, cdb, lid, activeSet, miners)
	createBlock(tb, cdb, blts)
	return miners
}

func createActiveSet(tb testing.TB, cdb *datastore.CachedDB, lid types.LayerID, activeSet []types.ATXID) []types.NodeID {
	var miners []types.NodeID
	for i, id := range activeSet {
		nodeID := types.BytesToNodeID([]byte(strconv.Itoa(i)))
		miners = append(miners, nodeID)
		atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: lid.GetEpoch(),
			},
			NumUnits: uint32(i + 1),
		}}
		atx.SetID(id)
		atx.SetEffectiveNumUnits(atx.NumUnits)
		atx.SetReceived(time.Now())
		atx.SmesherID = types.BytesToNodeID([]byte(strconv.Itoa(i)))
		vAtx, err := atx.Verify(0, 1)
		require.NoError(tb, err)
		require.NoError(tb, atxs.Add(cdb, vAtx))
	}
	return miners
}

func createMapWithSize(n int) map[types.NodeID]uint64 {
	m := make(map[types.NodeID]uint64)
	for i := 0; i < n; i++ {
		m[types.BytesToNodeID([]byte(strconv.Itoa(i)))] = uint64(i + 1)
	}
	return m
}

func TestCalcEligibility(t *testing.T) {
	nid := types.NodeID{1, 1}
	nonce := types.VRFPostIndex(1)

	t.Run("zero committee", func(t *testing.T) {
		o := defaultOracle(t)
		res, err := o.CalcEligibility(context.Background(), types.LayerID(50), 1, 0, nid, nonce, types.EmptyVrfSignature)
		require.ErrorIs(t, err, errZeroCommitteeSize)
		require.Equal(t, 0, int(res))
	})

	t.Run("empty active set", func(t *testing.T) {
		o := defaultOracle(t)
		o.mBeacon.EXPECT().GetBeacon(gomock.Any())
		lid := types.EpochID(5).FirstLayer()
		res, err := o.CalcEligibility(context.Background(), lid, 1, 1, nid, nonce, types.EmptyVrfSignature)
		require.ErrorIs(t, err, errEmptyActiveSet)
		require.Equal(t, 0, int(res))
	})

	t.Run("miner not active", func(t *testing.T) {
		o := defaultOracle(t)
		lid := types.EpochID(5).FirstLayer()
		createLayerData(t, o.cdb, lid.Sub(defLayersPerEpoch), 11)
		res, err := o.CalcEligibility(context.Background(), lid, 1, 1, nid, nonce, types.EmptyVrfSignature)
		require.ErrorIs(t, err, ErrNotActive)
		require.Equal(t, 0, int(res))
	})

	t.Run("beacon failure", func(t *testing.T) {
		o := defaultOracle(t)
		layer := types.EpochID(5).FirstLayer()
		miners := createLayerData(t, o.cdb, layer.Sub(defLayersPerEpoch), 5)
		errUnknown := errors.New("unknown")
		o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(types.EmptyBeacon, errUnknown).Times(1)

		res, err := o.CalcEligibility(context.Background(), layer, 0, 1, miners[0], nonce, types.EmptyVrfSignature)
		require.ErrorIs(t, err, errUnknown)
		require.Equal(t, 0, int(res))
	})

	t.Run("verify failure", func(t *testing.T) {
		o := defaultOracle(t)
		layer := types.EpochID(5).FirstLayer()
		miners := createLayerData(t, o.cdb, layer.Sub(defLayersPerEpoch), 5)
		o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(types.RandomBeacon(), nil).Times(1)
		o.mVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(false).Times(1)

		res, err := o.CalcEligibility(context.Background(), layer, 0, 1, miners[0], nonce, types.EmptyVrfSignature)
		require.NoError(t, err)
		require.Equal(t, 0, int(res))
	})

	t.Run("empty active with fallback", func(t *testing.T) {
		o := defaultOracle(t)
		o.mBeacon.EXPECT().GetBeacon(gomock.Any())
		lid := types.EpochID(5).FirstLayer().Add(o.cfg.ConfidenceParam)
		res, err := o.CalcEligibility(context.Background(), lid, 1, 1, nid, nonce, types.EmptyVrfSignature)
		require.ErrorIs(t, err, errEmptyActiveSet)
		require.Equal(t, 0, int(res))

		activeSet := types.RandomActiveSet(111)
		miners := createActiveSet(t, o.cdb, types.EpochID(4).FirstLayer(), activeSet)
		o.UpdateActiveSet(5, activeSet)
		o.mBeacon.EXPECT().GetBeacon(lid.GetEpoch()).Return(types.RandomBeacon(), nil)
		o.mVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true)
		_, err = o.CalcEligibility(context.Background(), lid, 1, 1, miners[0], nonce, types.EmptyVrfSignature)
		require.NoError(t, err)
	})

	t.Run("miner active", func(t *testing.T) {
		o := defaultOracle(t)
		lid := types.EpochID(5).FirstLayer()
		beacon := types.Beacon{1, 0, 0, 0}
		miners := createLayerData(t, o.cdb, lid.Sub(defLayersPerEpoch), 5)
		sigs := map[string]uint16{
			"0516a574aef37257d6811ea53ef55d4cbb0e14674900a0d5165bd6742513840d02442d979fdabc7059645d1e8f8a0f44d0db2aa90f23374dd74a3636d4ecdab7": 1,
			"73929b4b69090bb6133e2f8cd73989b35228e7e6d8c6745e4100d9c5eb48ca2624ee2889e55124195a130f74ea56e53a73a1c4dee60baa13ad3b1c0ed4f80d9c": 0,
			"e2c27ad65b752b763173b588518764b6c1e42896d57e0eabef9bcac68e07b87729a4ef9e5f17d8c1cb34ffd0d65ee9a7e63e63b77a7bcab1140a76fc04c271de": 0,
			"384460966938c87644987fe00c0f9d4f9a5e2dcd4bdc08392ed94203895ba325036725a22346e35aa707993babef716aa1b6b3dfc653a44cb23ac8f743cbbc3d": 1,
			"15c5f565a75888970059b070bfaed1998a9d423ddac9f6af83da51db02149044ea6aeb86294341c7a950ac5de2855bbebc11cc28b02c08bc903e4cf41439717d": 1,
		}
		for vrf, exp := range sigs {
			sig, err := hex.DecodeString(vrf)
			require.NoError(t, err)

			var vrfSig types.VrfSignature
			copy(vrfSig[:], sig)

			nonce := types.VRFPostIndex(1)
			o.mBeacon.EXPECT().GetBeacon(lid.GetEpoch()).Return(beacon, nil).Times(1)
			o.mVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).Times(1)
			res, err := o.CalcEligibility(context.Background(), lid, 1, 10, miners[0], nonce, vrfSig)
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
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			o := defaultOracle(t)
			o.mVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).AnyTimes()

			lid := types.EpochID(5).FirstLayer()
			beacon := types.Beacon{1, 0, 0, 0}
			miners := createLayerData(t, o.cdb, lid.Sub(defLayersPerEpoch), tc.numMiners)

			var eligibilityCount uint16
			for _, nodeID := range miners {
				sig := types.RandomVrfSignature()

				nonce := types.VRFPostIndex(rand.Uint64())
				o.mNonceFetcher.EXPECT().VRFNonce(nodeID, lid.GetEpoch()).Return(nonce, nil).Times(1)
				o.mBeacon.EXPECT().GetBeacon(lid.GetEpoch()).Return(beacon, nil).Times(2)
				res, err := o.CalcEligibility(context.Background(), lid, 1, committeeSize, nodeID, nonce, sig)
				require.NoError(t, err)

				valid, err := o.Validate(context.Background(), lid, 1, committeeSize, nodeID, sig, res)
				require.NoError(t, err)
				require.True(t, valid)

				eligibilityCount += res
			}

			diff := committeeSize - int(eligibilityCount)
			if diff < 0 {
				diff = -diff
			}
			t.Logf("diff=%d (%g%% of committeeSize)", diff, 100*float64(diff)/float64(committeeSize))
			require.Less(t, diff, committeeSize/10) // up to 10% difference
			// While it's theoretically possible to get a result higher than 10%, I've run this many times and haven't seen
			// anything higher than 6% and it's usually under 3%.
		})
	}
}

func BenchmarkOracle_CalcEligibility(b *testing.B) {
	r := require.New(b)

	o := defaultOracle(b)
	o.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil).AnyTimes()
	o.mVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	o.mNonceFetcher.EXPECT().VRFNonce(gomock.Any(), gomock.Any()).Return(types.VRFPostIndex(1), nil).AnyTimes()
	numOfMiners := 2000
	committeeSize := 800

	lid := types.EpochID(5).FirstLayer()
	createLayerData(b, o.cdb, lid, numOfMiners)

	var eligibilityCount uint16
	var nodeIDs []types.NodeID
	for pubkey := range createMapWithSize(b.N) {
		nodeIDs = append(nodeIDs, pubkey)
	}
	b.ResetTimer()
	for _, nodeID := range nodeIDs {
		nonce := types.VRFPostIndex(rand.Uint64())
		res, err := o.CalcEligibility(context.Background(), lid, 1, committeeSize, nodeID, nonce, types.EmptyVrfSignature)

		if err == nil {
			valid, err := o.Validate(context.Background(), lid, 1, committeeSize, nodeID, types.EmptyVrfSignature, res)
			r.NoError(err)
			r.True(valid)
		}

		eligibilityCount += res
	}
}

func Test_VrfSignVerify(t *testing.T) {
	// eligibility of the proof depends on the identity
	rng := rand.New(rand.NewSource(5))

	signer, err := signing.NewEdSigner(signing.WithKeyFromRand(rng))
	require.NoError(t, err)

	o := defaultOracle(t)
	o.vrfSigner, err = signer.VRFSigner()
	require.NoError(t, err)
	nid := signer.NodeID()
	nonce := types.VRFPostIndex(1)

	lid := types.EpochID(5).FirstLayer()
	prevEpoch := lid.GetEpoch() - 1
	o.mBeacon.EXPECT().GetBeacon(lid.GetEpoch()).Return(types.Beacon{1, 0, 0, 0}, nil).AnyTimes()
	o.mNonceFetcher.EXPECT().VRFNonce(nid, lid.GetEpoch()).Return(nonce, nil).Times(1)

	numMiners := 2
	activeSet := types.RandomActiveSet(numMiners)
	atx1 := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
		NIPostChallenge: types.NIPostChallenge{
			PublishEpoch: prevEpoch,
		},
		NumUnits: 1 * 1024,
	}}
	atx1.SetID(activeSet[0])
	atx1.SetEffectiveNumUnits(atx1.NumUnits)
	atx1.SetReceived(time.Now())
	atx1.SmesherID = signer.NodeID()
	vAtx1, err := atx1.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(o.cdb, vAtx1))

	signer2, err := signing.NewEdSigner(signing.WithKeyFromRand(rng))
	require.NoError(t, err)

	atx2 := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
		NIPostChallenge: types.NIPostChallenge{
			PublishEpoch: prevEpoch,
		},
		NumUnits: 9 * 1024,
	}}
	atx2.SetID(activeSet[1])
	atx2.SetEffectiveNumUnits(atx2.NumUnits)
	atx2.SetReceived(time.Now())
	atx2.SmesherID = signer2.NodeID()
	vAtx2, err := atx2.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(o.cdb, vAtx2))
	miners := []types.NodeID{atx1.SmesherID, atx2.SmesherID}
	createBlock(t, o.cdb, createBallots(t, o.cdb, lid.Sub(defLayersPerEpoch), activeSet, miners))

	o.vrfVerifier = signing.NewVRFVerifier()

	proof, err := o.Proof(context.Background(), nonce, lid, 1)
	require.NoError(t, err)

	res, err := o.CalcEligibility(context.Background(), lid, 1, 10, nid, nonce, proof)
	require.NoError(t, err)
	require.Equal(t, 1, int(res))

	valid, err := o.Validate(context.Background(), lid, 1, 10, nid, proof, 1)
	require.NoError(t, err)
	require.True(t, valid)
}

func Test_Proof_BeaconError(t *testing.T) {
	o := defaultOracle(t)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	o.vrfSigner, err = signer.VRFSigner()
	require.NoError(t, err)

	layer := types.LayerID(2)
	errUnknown := errors.New("unknown")
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(types.EmptyBeacon, errUnknown).Times(1)

	_, err = o.Proof(context.Background(), types.VRFPostIndex(rand.Uint64()), layer, 3)
	require.ErrorIs(t, err, errUnknown)
}

func Test_Proof(t *testing.T) {
	o := defaultOracle(t)
	layer := types.LayerID(2)
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(types.Beacon{1, 0, 0, 0}, nil)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	vrfSigner, err := signer.VRFSigner()
	require.NoError(t, err)

	o.vrfSigner = vrfSigner
	sig, err := o.Proof(context.Background(), types.VRFPostIndex(rand.Uint64()), layer, 3)
	require.Nil(t, err)
	require.NotNil(t, sig)
}

func TestOracle_IsIdentityActive(t *testing.T) {
	o := defaultOracle(t)
	layer := types.LayerID(defLayersPerEpoch * 4)
	numMiners := 2
	miners := createLayerData(t, o.cdb, layer.Sub(defLayersPerEpoch), numMiners)
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
	msg, err := o.buildVRFMessage(context.Background(), types.VRFPostIndex(1), types.LayerID(1), 1)
	require.ErrorIs(t, err, errUnknown)
	require.Nil(t, msg)
}

func TestBuildVRFMessage(t *testing.T) {
	o := defaultOracle(t)
	nonce := types.VRFPostIndex(2)
	firstLayer := types.LayerID(1)
	secondLayer := firstLayer.Add(1)
	beacon := types.RandomBeacon()
	o.mBeacon.EXPECT().GetBeacon(firstLayer.GetEpoch()).Return(beacon, nil).Times(1)
	m1, err := o.buildVRFMessage(context.Background(), nonce, firstLayer, 2)
	require.NoError(t, err)

	// check not same for different round
	o.mBeacon.EXPECT().GetBeacon(firstLayer.GetEpoch()).Return(beacon, nil).Times(1)
	m3, err := o.buildVRFMessage(context.Background(), nonce, firstLayer, 3)
	require.NoError(t, err)
	require.NotEqual(t, m1, m3)

	// check not same for different layer
	o.mBeacon.EXPECT().GetBeacon(firstLayer.GetEpoch()).Return(beacon, nil).Times(1)
	m4, err := o.buildVRFMessage(context.Background(), nonce, secondLayer, 2)
	require.NoError(t, err)
	require.NotEqual(t, m1, m4)

	// check same call returns same result
	o.mBeacon.EXPECT().GetBeacon(firstLayer.GetEpoch()).Return(beacon, nil).Times(1)
	m5, err := o.buildVRFMessage(context.Background(), nonce, firstLayer, 2)
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
			_, err := o.buildVRFMessage(context.Background(), types.VRFPostIndex(1), firstLayer, uint32(x%expectAdd))
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
	createLayerData(t, o.cdb, targetEpoch.FirstLayer(), numMiners)

	aset, err := o.actives(context.Background(), layer)
	require.NoError(t, err)
	require.Equal(t, createMapWithSize(numMiners), aset.set)

	got, err := o.ActiveSet(context.Background(), targetEpoch)
	require.NoError(t, err)
	require.Len(t, got, len(aset.set))
	for _, id := range got {
		atx, err := o.cdb.GetAtxHeader(id)
		require.NoError(t, err)
		require.Contains(t, aset.set, atx.NodeID)
		delete(aset.set, atx.NodeID)
	}
}

func TestActives(t *testing.T) {
	numMiners := 5
	t.Run("genesis bootstrap", func(t *testing.T) {
		o := defaultOracle(t)
		first := types.GetEffectiveGenesis().Add(1)
		bootstrap := types.RandomActiveSet(numMiners)
		createActiveSet(t, o.cdb, types.EpochID(1).FirstLayer(), bootstrap)
		o.UpdateActiveSet(types.GetEffectiveGenesis().GetEpoch()+1, bootstrap)

		for lid := types.LayerID(0); lid.Before(first); lid = lid.Add(1) {
			activeSet, err := o.actives(context.Background(), lid)
			require.ErrorIs(t, err, errEmptyActiveSet)
			require.Nil(t, activeSet)
		}
		activeSet, err := o.actives(context.Background(), first)
		require.NoError(t, err)
		require.Equal(t, createMapWithSize(numMiners), activeSet.set)
	})
	t.Run("steady state", func(t *testing.T) {
		numMiners++
		o := defaultOracle(t)
		o.mBeacon.EXPECT().GetBeacon(gomock.Any())
		layer := types.EpochID(4).FirstLayer()
		createLayerData(t, o.cdb, layer, numMiners)

		start := layer.Add(o.cfg.ConfidenceParam)
		activeSet, err := o.actives(context.Background(), start)
		require.NoError(t, err)
		require.Equal(t, createMapWithSize(numMiners), activeSet.set)
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
		createLayerData(t, o.cdb, layer, numMiners)
		fallback := types.RandomActiveSet(numMiners + 1)
		createActiveSet(t, o.cdb, types.EpochID(3).FirstLayer(), fallback)
		o.UpdateActiveSet(end.GetEpoch(), fallback)

		for lid := layer; lid.Before(end); lid = lid.Add(1) {
			got, err := o.actives(context.Background(), lid)
			require.ErrorIs(t, err, errEmptyActiveSet)
			require.Nil(t, got)
		}
		got, err := o.actives(context.Background(), end)
		require.NoError(t, err)
		require.Equal(t, createMapWithSize(numMiners+1), got.set)
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
		createLayerData(t, o.cdb, layer, numMiners)
		fallback := types.RandomActiveSet(numMiners + 1)
		createActiveSet(t, o.cdb, types.EpochID(3).FirstLayer(), fallback)
		o.UpdateActiveSet(layer.GetEpoch(), fallback)

		got, err := o.actives(context.Background(), layer)
		require.NoError(t, err)
		require.Equal(t, createMapWithSize(numMiners+1), got.set)
		got2, err := o.actives(context.Background(), layer+1)
		require.NoError(t, err)
		require.Equal(t, got2, got)
	})
}

func TestActives_ConcurrentCalls(t *testing.T) {
	r := require.New(t)
	o := defaultOracle(t)
	layer := types.LayerID(100)
	createLayerData(t, o.cdb, layer.Sub(defLayersPerEpoch), 5)

	mc := NewMockactiveSetCache(gomock.NewController(t))
	firstCall := true
	mc.EXPECT().Get(layer.GetEpoch() - 1).DoAndReturn(
		func(key any) (any, bool) {
			if firstCall {
				firstCall = false
				return nil, false
			}
			aset := cachedActiveSet{set: createMapWithSize(5)}
			for _, value := range aset.set {
				aset.total += value
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

func TestActiveSetDD(t *testing.T) {
	t.Parallel()

	target := types.EpochID(4)
	bgen := func(id types.BallotID, lid types.LayerID, node types.NodeID, beacon types.Beacon, atxs []types.ATXID, option ...func(*types.Ballot)) types.Ballot {
		ballot := types.Ballot{}
		ballot.Layer = lid
		ballot.EpochData = &types.EpochData{Beacon: beacon}
		ballot.ActiveSet = atxs
		ballot.SmesherID = node
		ballot.SetID(id)
		for _, opt := range option {
			opt(&ballot)
		}
		return ballot
	}
	agen := func(id types.ATXID, node types.NodeID, option ...func(*types.VerifiedActivationTx)) *types.VerifiedActivationTx {
		atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{},
		}}
		atx.PublishEpoch = target - 1
		atx.SmesherID = node
		atx.SetID(id)
		atx.SetEffectiveNumUnits(1)
		atx.SetReceived(time.Time{}.Add(1))
		verified, err := atx.Verify(0, 1)
		require.NoError(t, err)
		for _, opt := range option {
			opt(verified)
		}
		return verified
	}

	for _, tc := range []struct {
		desc    string
		beacon  types.Beacon // local beacon
		ballots []types.Ballot
		atxs    []*types.VerifiedActivationTx
		expect  any
	}{
		{
			"merged activesets",
			types.Beacon{1},
			[]types.Ballot{
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
			[]*types.VerifiedActivationTx{
				agen(types.ATXID{1}, types.NodeID{1}),
				agen(types.ATXID{2}, types.NodeID{2}),
				agen(types.ATXID{3}, types.NodeID{3}),
			},
			[]types.ATXID{{1}, {2}, {3}},
		},
		{
			"filter by beacon",
			types.Beacon{1},
			[]types.Ballot{
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
			[]*types.VerifiedActivationTx{
				agen(types.ATXID{1}, types.NodeID{1}),
				agen(types.ATXID{2}, types.NodeID{2}),
			},
			[]types.ATXID{{1}, {2}},
		},
		{
			"no local beacon",
			types.EmptyBeacon,
			[]types.Ballot{
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
			[]*types.VerifiedActivationTx{},
			"not found",
		},
		{
			"unknown atxs",
			types.Beacon{1},
			[]types.Ballot{
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
			[]*types.VerifiedActivationTx{},
			"get ATX",
		},
		{
			"ballot no epoch data",
			types.Beacon{1},
			[]types.Ballot{
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
			[]*types.VerifiedActivationTx{
				agen(types.ATXID{2}, types.NodeID{2}),
				agen(types.ATXID{3}, types.NodeID{3}),
			},
			[]types.ATXID{{2}, {3}},
		},
		{
			"wrong target epoch",
			types.Beacon{1},
			[]types.Ballot{
				bgen(
					types.BallotID{1},
					target.FirstLayer(),
					types.NodeID{1},
					types.Beacon{1},
					[]types.ATXID{{1}},
				),
			},
			[]*types.VerifiedActivationTx{
				agen(types.ATXID{1}, types.NodeID{1}, func(verified *types.VerifiedActivationTx) {
					verified.PublishEpoch = target
				}),
			},
			"no epoch atx found",
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			oracle := defaultOracle(t)
			for _, ballot := range tc.ballots {
				require.NoError(t, ballots.Add(oracle.cdb, &ballot))
			}
			for _, atx := range tc.atxs {
				require.NoError(t, atxs.Add(oracle.cdb, atx))
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

func FuzzVrfMessageConsistency(f *testing.F) {
	tester.FuzzConsistency[VrfMessage](f)
}

func FuzzVrfMessageSafety(f *testing.F) {
	tester.FuzzSafety[VrfMessage](f)
}
