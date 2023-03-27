package eligibility

import (
	"context"
	"encoding/hex"
	"errors"
	"math/rand"
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
	"github.com/spacemeshos/go-spacemesh/system/mocks"
)

const (
	defLayersPerEpoch uint32 = 10
	confidenceParam   uint32 = 25
	epochOffset       uint32 = 3
)

type testOracle struct {
	*Oracle
	mBeacon       *mocks.MockBeaconGetter
	mVerifier     *MockvrfVerifier
	mNonceFetcher *MocknonceFetcher
}

func defaultOracle(t testing.TB) *testOracle {
	types.SetLayersPerEpoch(defLayersPerEpoch)
	lg := logtest.New(t)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)

	ctrl := gomock.NewController(t)
	mb := mocks.NewMockBeaconGetter(ctrl)
	verifier := NewMockvrfVerifier(ctrl)
	nonceFetcher := NewMocknonceFetcher(ctrl)

	to := &testOracle{
		Oracle: New(mb, cdb, verifier, nil, defLayersPerEpoch, config.Config{ConfidenceParam: confidenceParam, EpochOffset: epochOffset}, lg,
			withNonceFetcher(nonceFetcher),
		),
		mBeacon:       mb,
		mVerifier:     verifier,
		mNonceFetcher: nonceFetcher,
	}
	return to
}

func createLayerData(tb testing.TB, cdb *datastore.CachedDB, lid types.LayerID, beacon types.Beacon, numMiners int) {
	tb.Helper()
	signer, err := signing.NewEdSigner()
	require.NoError(tb, err)

	activeSet := types.RandomActiveSet(numMiners)
	start, end := safeLayerRange(lid, confidenceParam, defLayersPerEpoch, epochOffset)
	for lyr := start; !lyr.After(end); lyr = lyr.Add(1) {
		for _, atx := range activeSet {
			b := types.RandomBallot()
			b.Layer = lyr
			b.AtxID = atx
			b.RefBallot = types.EmptyBallotID
			b.EpochData = &types.EpochData{ActiveSet: activeSet, Beacon: beacon}
			b.Signature = signer.Sign(signing.HARE, b.SignedBytes())
			b.SetSmesherID(signer.NodeID())
			require.NoError(tb, b.Initialize())
			require.NoError(tb, ballots.Add(cdb, b))
		}
	}

	prevEpoch := lid.GetEpoch() - 1
	createActiveSet(tb, cdb, prevEpoch.FirstLayer(), activeSet)
}

func createActiveSet(tb testing.TB, cdb *datastore.CachedDB, lid types.LayerID, activeSet []types.ATXID) {
	for i, id := range activeSet {
		nodeID := types.BytesToNodeID([]byte(strconv.Itoa(i)))
		atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: lid,
			},
			NumUnits: uint32(i + 1),
		}}
		atx.SetID(&id)
		atx.SetNodeID(&nodeID)
		atx.SetEffectiveNumUnits(atx.NumUnits)
		atx.SetReceived(time.Now())
		vAtx, err := atx.Verify(0, 1)
		require.NoError(tb, err)
		require.NoError(tb, atxs.Add(cdb, vAtx))
	}
}

func beaconWithValOne() types.Beacon {
	return types.Beacon{1, 0, 0, 0}
}

func createMapWithSize(n int) map[types.NodeID]uint64 {
	m := make(map[types.NodeID]uint64)
	for i := 0; i < n; i++ {
		m[types.BytesToNodeID([]byte(strconv.Itoa(i)))] = uint64(i + 1)
	}
	return m
}

func TestCalcEligibility_ZeroCommittee(t *testing.T) {
	o := defaultOracle(t)
	nid := types.NodeID{1, 1}
	nonce := types.VRFPostIndex(1)
	res, err := o.CalcEligibility(context.Background(), types.NewLayerID(50), 1, 0, nid, nonce, []byte{})
	require.ErrorIs(t, err, errZeroCommitteeSize)
	require.Equal(t, 0, int(res))
}

func TestCalcEligibility_BeaconFailure(t *testing.T) {
	o := defaultOracle(t)
	nid := types.NodeID{1, 1}
	nonce := types.VRFPostIndex(1)
	layer := types.NewLayerID(50)
	errUnknown := errors.New("unknown")
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(types.EmptyBeacon, errUnknown).Times(1)

	res, err := o.CalcEligibility(context.Background(), layer, 0, 1, nid, nonce, []byte{})
	require.ErrorIs(t, err, errUnknown)
	require.Equal(t, 0, int(res))
}

func TestCalcEligibility_VerifyFailure(t *testing.T) {
	o := defaultOracle(t)
	nid := types.NodeID{1, 1}
	nonce := types.VRFPostIndex(1)
	layer := types.NewLayerID(50)
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(types.RandomBeacon(), nil).Times(1)
	o.mVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(false).Times(1)

	res, err := o.CalcEligibility(context.Background(), layer, 0, 1, nid, nonce, []byte{})
	require.NoError(t, err)
	require.Equal(t, 0, int(res))
}

func TestCalcEligibility_EmptyActiveSet(t *testing.T) {
	o := defaultOracle(t)
	nid := types.NodeID{1, 1}
	nonce := types.VRFPostIndex(1)
	layer := types.NewLayerID(40)
	start, end := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	require.Equal(t, start, end)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	beacon := types.RandomBeacon()
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(beacon, nil).Times(1)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)
	o.mVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).Times(1)

	numMiners := 5
	activeSet := types.RandomActiveSet(numMiners)

	for _, atx := range activeSet {
		b := types.RandomBallot()
		b.AtxID = atx
		b.RefBallot = types.EmptyBallotID
		b.EpochData = &types.EpochData{ActiveSet: activeSet, Beacon: beacon}
		b.Signature = signer.Sign(signing.HARE, b.SignedBytes())
		require.NoError(t, b.Initialize())
		b.SetSmesherID(signer.NodeID())
		require.NoError(t, ballots.Add(o.cdb, b))
	}
	res, err := o.CalcEligibility(context.Background(), layer, 1, 1, nid, nonce, []byte{})
	require.ErrorIs(t, err, errEmptyActiveSet)
	require.Equal(t, 0, int(res))
}

func TestCalcEligibility_EligibleFromHareActiveSet(t *testing.T) {
	o := defaultOracle(t)
	layer := types.NewLayerID(50)
	beacon := beaconWithValOne()
	createLayerData(t, o.cdb, layer, beacon, 5)

	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)

	sigs := map[string]uint16{
		"0516a574aef37257d6811ea53ef55d4cbb0e14674900a0d5165bd6742513840d02442d979fdabc7059645d1e8f8a0f44d0db2aa90f23374dd74a3636d4ecdab7": 1,
		"73929b4b69090bb6133e2f8cd73989b35228e7e6d8c6745e4100d9c5eb48ca2624ee2889e55124195a130f74ea56e53a73a1c4dee60baa13ad3b1c0ed4f80d9c": 0,
		"e2c27ad65b752b763173b588518764b6c1e42896d57e0eabef9bcac68e07b87729a4ef9e5f17d8c1cb34ffd0d65ee9a7e63e63b77a7bcab1140a76fc04c271de": 0,
		"384460966938c87644987fe00c0f9d4f9a5e2dcd4bdc08392ed94203895ba325036725a22346e35aa707993babef716aa1b6b3dfc653a44cb23ac8f743cbbc3d": 1,
		"15c5f565a75888970059b070bfaed1998a9d423ddac9f6af83da51db02149044ea6aeb86294341c7a950ac5de2855bbebc11cc28b02c08bc903e4cf41439717d": 1,
	}
	for s, exp := range sigs {
		sig, err := hex.DecodeString(s)
		require.NoError(t, err)
		nid := types.BytesToNodeID([]byte("0"))
		nonce := types.VRFPostIndex(1)
		o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(beacon, nil).Times(1)
		o.mVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).Times(1)
		res, err := o.CalcEligibility(context.Background(), layer, 1, 10, nid, nonce, sig)
		require.NoError(t, err, s)
		require.Equal(t, exp, res, s)
	}
}

func TestCalcEligibility_EligibleFromTortoiseActiveSet(t *testing.T) {
	o := defaultOracle(t)
	layer := types.NewLayerID(40)
	beacon := beaconWithValOne()
	start, end := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	require.Equal(t, start, end)

	sigs := map[string]uint16{
		"0516a574aef37257d6811ea53ef55d4cbb0e14674900a0d5165bd6742513840d02442d979fdabc7059645d1e8f8a0f44d0db2aa90f23374dd74a3636d4ecdab7": 1,
		"73929b4b69090bb6133e2f8cd73989b35228e7e6d8c6745e4100d9c5eb48ca2624ee2889e55124195a130f74ea56e53a73a1c4dee60baa13ad3b1c0ed4f80d9c": 0,
		"e2c27ad65b752b763173b588518764b6c1e42896d57e0eabef9bcac68e07b87729a4ef9e5f17d8c1cb34ffd0d65ee9a7e63e63b77a7bcab1140a76fc04c271de": 0,
		"384460966938c87644987fe00c0f9d4f9a5e2dcd4bdc08392ed94203895ba325036725a22346e35aa707993babef716aa1b6b3dfc653a44cb23ac8f743cbbc3d": 1,
		"15c5f565a75888970059b070bfaed1998a9d423ddac9f6af83da51db02149044ea6aeb86294341c7a950ac5de2855bbebc11cc28b02c08bc903e4cf41439717d": 1,
	}
	o.mVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).AnyTimes()

	numMiners := 5
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(beacon, nil).Times(numMiners)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)
	activeSet := types.RandomActiveSet(numMiners)
	// there is no cache for tortoise active set. so each signature will cause 2 calls to GetEpochAtxs() and 2*numMiners calls to GetAtxHeader()
	prevEpoch := layer.GetEpoch() - 1
	createActiveSet(t, o.cdb, prevEpoch.FirstLayer(), activeSet)
	for s, exp := range sigs {
		sig, err := hex.DecodeString(s)
		require.NoError(t, err)
		nid := types.BytesToNodeID([]byte("0"))
		nonce := types.VRFPostIndex(1)
		res, err := o.CalcEligibility(context.Background(), layer, 1, 10, nid, nonce, sig)
		require.NoError(t, err, s)
		require.Equal(t, exp, res, s)
	}
}

func TestCalcEligibility_WithSpaceUnits(t *testing.T) {
	r := require.New(t)
	numOfMiners := 50
	committeeSize := 800

	o := defaultOracle(t)
	o.mVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).AnyTimes()

	layer := types.NewLayerID(50)
	beacon := beaconWithValOne()
	createLayerData(t, o.cdb, layer, beacon, numOfMiners)
	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)

	var eligibilityCount uint16
	sig := make([]byte, 80)
	for nodeID := range createMapWithSize(numOfMiners) {
		_, err := rand.Read(sig)
		r.NoError(err)

		nonce := types.VRFPostIndex(rand.Uint64())
		o.mNonceFetcher.EXPECT().VRFNonce(nodeID, layer.GetEpoch()).Return(nonce, nil).Times(1)
		o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(beacon, nil).Times(2)
		res, err := o.CalcEligibility(context.Background(), layer, 1, committeeSize, nodeID, nonce, sig)
		r.NoError(err)

		valid, err := o.Validate(context.Background(), layer, 1, committeeSize, nodeID, sig, res)
		r.NoError(err)
		r.True(valid)

		eligibilityCount += res
	}

	diff := committeeSize - int(eligibilityCount)
	if diff < 0 {
		diff = -diff
	}
	t.Logf("diff=%d (%g%% of committeeSize)", diff, 100*float64(diff)/float64(committeeSize))
	r.Less(diff, committeeSize/10) // up to 10% difference
	// While it's theoretically possible to get a result higher than 10%, I've run this many times and haven't seen
	// anything higher than 6% and it's usually under 3%.
}

func Test_CalcEligibility_MainnetParams(t *testing.T) {
	r := require.New(t)
	numOfMiners := 2000
	committeeSize := 800
	rng := rand.New(rand.NewSource(999))

	o := defaultOracle(t)
	o.mVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(true).AnyTimes()

	layer := types.NewLayerID(50)
	beacon := types.RandomBeacon()
	createLayerData(t, o.cdb, layer, beacon, numOfMiners)
	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)

	var eligibilityCount uint16
	sig := make([]byte, 64)
	for i := 0; i < numOfMiners; i++ {
		n, err := rng.Read(sig)
		r.NoError(err)
		r.Equal(64, n)
		nodeID := types.BytesToNodeID([]byte(strconv.Itoa(i)))
		nonce := types.VRFPostIndex(rng.Uint64())
		o.mNonceFetcher.EXPECT().VRFNonce(nodeID, layer.GetEpoch()).Return(nonce, nil).Times(1)
		o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(beacon, nil).Times(2)

		res, err := o.CalcEligibility(context.Background(), layer, 1, committeeSize, nodeID, nonce, sig)
		r.NoError(err)

		valid, err := o.Validate(context.Background(), layer, 1, committeeSize, nodeID, sig, res)
		r.NoError(err)
		r.True(valid)

		eligibilityCount += res
	}

	diff := committeeSize - int(eligibilityCount)
	if diff < 0 {
		diff = -diff
	}
	t.Logf("diff=%d (%g%% of committeeSize)", diff, 100*float64(diff)/float64(committeeSize))
	r.Less(diff, committeeSize/10) // up to 10% difference
	// While it's theoretically possible to get a result higher than 10%, I've run this many times and haven't seen
	// anything higher than 6% and it's usually under 3%.
}

func BenchmarkOracle_CalcEligibility(b *testing.B) {
	r := require.New(b)

	o := defaultOracle(b)
	numOfMiners := 2000
	committeeSize := 800

	layer := types.NewLayerID(50)
	beacon := types.RandomBeacon()
	createLayerData(b, o.cdb, layer, beacon, numOfMiners)
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(beacon, nil).Times(1)
	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)

	var eligibilityCount uint16
	sig := make([]byte, 64)

	var nodeIDs []types.NodeID
	for pubkey := range createMapWithSize(b.N) {
		nodeIDs = append(nodeIDs, pubkey)
	}
	b.ResetTimer()
	for _, nodeID := range nodeIDs {
		nonce := types.VRFPostIndex(rand.Uint64())
		res, err := o.CalcEligibility(context.Background(), layer, 1, committeeSize, nodeID, nonce, sig)

		if err == nil {
			valid, err := o.Validate(context.Background(), layer, 1, committeeSize, nodeID, sig, res)
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

	layer := types.NewLayerID(50)
	start, end := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(beaconWithValOne(), nil).Times(int(end.Difference(start)))
	o.mNonceFetcher.EXPECT().VRFNonce(nid, layer.GetEpoch()).Return(nonce, nil).Times(1)
	beacon := types.RandomBeacon()
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)

	numMiners := 2
	activeSet := types.RandomActiveSet(numMiners)
	for lyr := start; !lyr.After(end); lyr = lyr.Add(1) {
		for _, atx := range activeSet {
			b := types.RandomBallot()
			b.Layer = lyr
			b.AtxID = atx
			b.RefBallot = types.EmptyBallotID
			b.EpochData = &types.EpochData{ActiveSet: activeSet, Beacon: beacon}
			b.Signature = signer.Sign(signing.HARE, b.SignedBytes())
			b.SetSmesherID(signer.NodeID())
			require.NoError(t, b.Initialize())
			require.NoError(t, ballots.Add(o.cdb, b))
		}
	}
	prevEpoch := layer.GetEpoch() - 1

	atx1 := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
		NIPostChallenge: types.NIPostChallenge{
			PubLayerID: prevEpoch.FirstLayer(),
		},
		NumUnits: 1 * 1024,
	}}
	atx1.SetID(&activeSet[0])
	atx1.SetNodeID(&nid)
	atx1.SetEffectiveNumUnits(atx1.NumUnits)
	atx1.SetReceived(time.Now())
	vAtx1, err := atx1.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(o.cdb, vAtx1))

	atx2 := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
		NIPostChallenge: types.NIPostChallenge{
			PubLayerID: prevEpoch.FirstLayer(),
		},
		NumUnits: 9 * 1024,
	}}
	atx2.SetID(&activeSet[1])
	atx2.SetNodeID(&types.NodeID{1})
	atx2.SetEffectiveNumUnits(atx2.NumUnits)
	atx2.SetReceived(time.Now())
	vAtx2, err := atx2.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(o.cdb, vAtx2))

	o.vrfVerifier = signing.NewVRFVerifier()

	proof, err := o.Proof(context.Background(), nonce, layer, 1)
	require.NoError(t, err)

	res, err := o.CalcEligibility(context.Background(), layer, 1, 10, nid, nonce, proof)
	require.NoError(t, err)
	require.Equal(t, 1, int(res))

	valid, err := o.Validate(context.Background(), layer, 1, 10, nid, proof, 1)
	require.NoError(t, err)
	require.True(t, valid)
}

func Test_Proof_BeaconError(t *testing.T) {
	o := defaultOracle(t)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	o.vrfSigner, err = signer.VRFSigner()
	require.NoError(t, err)

	layer := types.NewLayerID(2)
	errUnknown := errors.New("unknown")
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(types.EmptyBeacon, errUnknown).Times(1)

	sig, err := o.Proof(context.Background(), types.VRFPostIndex(rand.Uint64()), layer, 3)
	require.Nil(t, sig)
	require.ErrorIs(t, err, errUnknown)
}

func Test_Proof(t *testing.T) {
	o := defaultOracle(t)
	layer := types.NewLayerID(2)
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(beaconWithValOne(), nil).Times(1)

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
	layer := types.NewLayerID(40)
	start, end := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	require.Equal(t, start, end)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	beacon := types.RandomBeacon()
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)

	numMiners := 2
	activeSet := types.RandomActiveSet(numMiners)
	for _, atx := range activeSet {
		b := types.RandomBallot()
		b.Layer = start
		b.AtxID = atx
		b.RefBallot = types.EmptyBallotID
		b.EpochData = &types.EpochData{ActiveSet: activeSet, Beacon: beacon}
		b.Signature = signer.Sign(signing.HARE, b.SignedBytes())
		require.NoError(t, b.Initialize())
		b.SetSmesherID(signer.NodeID())
		require.NoError(t, ballots.Add(o.cdb, b))
	}
	prevEpoch := layer.GetEpoch() - 1

	atx1 := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
		NIPostChallenge: types.NIPostChallenge{
			PubLayerID: prevEpoch.FirstLayer(),
		},
		NumUnits: 1 * 1024,
	}}
	atx1.SetID(&activeSet[0])
	atx1.SetNodeID(&types.NodeID{1})
	atx1.SetEffectiveNumUnits(atx1.NumUnits)
	atx1.SetReceived(time.Now())
	vAtx1, err := atx1.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(o.cdb, vAtx1))

	atx2 := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
		NIPostChallenge: types.NIPostChallenge{
			PubLayerID: prevEpoch.FirstLayer(),
		},
		NumUnits: 9 * 1024,
	}}
	atx2.SetID(&activeSet[1])
	atx2.SetNodeID(&types.NodeID{2})
	atx2.SetEffectiveNumUnits(atx2.NumUnits)
	atx2.SetReceived(time.Now())
	vAtx2, err := atx2.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(o.cdb, vAtx2))

	for _, edID := range []types.NodeID{atx1.NodeID(), atx2.NodeID()} {
		v, err := o.IsIdentityActiveOnConsensusView(context.Background(), edID, layer)
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
	msg, err := o.buildVRFMessage(context.Background(), types.VRFPostIndex(1), types.NewLayerID(1), 1)
	require.ErrorIs(t, err, errUnknown)
	require.Nil(t, msg)
}

func TestBuildVRFMessage(t *testing.T) {
	o := defaultOracle(t)
	nonce := types.VRFPostIndex(2)
	firstLayer := types.NewLayerID(1)
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
	firstLayer := types.NewLayerID(1)
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

func TestSafeLayerRange(t *testing.T) {
	types.SetLayersPerEpoch(defLayersPerEpoch)
	safetyParam := uint32(10)
	effGenesis := types.GetEffectiveGenesis()
	testCases := []struct {
		// input
		targetLayer    types.LayerID
		safetyParam    uint32
		layersPerEpoch uint32
		epochOffset    uint32
		// expected output
		safeLayerStart types.LayerID
		safeLayerEnd   types.LayerID
	}{
		// a target layer prior to effective genesis returns effective genesis
		{types.NewLayerID(0), safetyParam, defLayersPerEpoch, 1, effGenesis, effGenesis},
		// large safety param also returns effective genesis
		{types.NewLayerID(100), 100, defLayersPerEpoch, 1, effGenesis, effGenesis},
		// safe layer in first safetyParam + epochOffset layers of epoch, rewinds one epoch further (two prior to target)
		{types.NewLayerID(100), safetyParam, defLayersPerEpoch, 1, types.NewLayerID(80), types.NewLayerID(81)},
		// zero offset
		{types.NewLayerID(100), safetyParam, defLayersPerEpoch, 0, types.NewLayerID(90), types.NewLayerID(90)},
		// safetyParam < layersPerEpoch means only look back one epoch
		{types.NewLayerID(100), safetyParam - 1, defLayersPerEpoch, 1, types.NewLayerID(90), types.NewLayerID(91)},
		// larger epochOffset looks back further
		{types.NewLayerID(100), safetyParam, defLayersPerEpoch, 5, types.NewLayerID(80), types.NewLayerID(85)},
		// smaller safety param returns one epoch prior
		{types.NewLayerID(100), 5, defLayersPerEpoch, 5, types.NewLayerID(90), types.NewLayerID(95)},
		// targetLayer within safetyParam returns one epoch prior
		{types.NewLayerID(105), 5, defLayersPerEpoch, 5, types.NewLayerID(90), types.NewLayerID(95)},
		// targetLayer after safetyParam+epochOffset returns start of same epoch
		{types.NewLayerID(105), 2, defLayersPerEpoch, 2, types.NewLayerID(100), types.NewLayerID(102)},
	}
	for _, testCase := range testCases {
		sls, sle := safeLayerRange(testCase.targetLayer, testCase.safetyParam, testCase.layersPerEpoch, testCase.epochOffset)
		require.Equal(t, testCase.safeLayerStart, sls, "got incorrect safeLayerStart")
		require.Equal(t, testCase.safeLayerEnd, sle, "got incorrect safeLayerEnd")
	}
}

func TestActives_HareActiveSet(t *testing.T) {
	o := defaultOracle(t)
	numMiners := 5
	layer := types.NewLayerID(50)
	beacon := types.RandomBeacon()
	createLayerData(t, o.cdb, layer, beacon, numMiners)
	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)

	activeSet, err := o.actives(context.Background(), layer)
	require.NoError(t, err)
	require.Equal(t, createMapWithSize(numMiners), activeSet)
}

func TestActives_HareActiveSetDifferentBeacon(t *testing.T) {
	o := defaultOracle(t)
	layer := types.NewLayerID(50)
	start, end := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	beacon := types.RandomBeacon()
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)
	numMiners := 5
	atxIDs := types.RandomActiveSet(numMiners)
	badBeacon := types.RandomBeacon()
	badBeaconATX := atxIDs[len(atxIDs)-1]
	for lyr := start; !lyr.After(end); lyr = lyr.Add(1) {
		for _, atx := range atxIDs {
			b := types.RandomBallot()
			b.Layer = lyr
			b.AtxID = atx
			b.RefBallot = types.EmptyBallotID
			if atx == badBeaconATX {
				b.EpochData = &types.EpochData{ActiveSet: atxIDs, Beacon: badBeacon}
			} else {
				b.EpochData = &types.EpochData{ActiveSet: atxIDs, Beacon: beacon}
			}
			b.Signature = signer.Sign(signing.HARE, b.SignedBytes())
			b.SetSmesherID(signer.NodeID())
			require.NoError(t, b.Initialize())
			require.NoError(t, ballots.Add(o.cdb, b))
		}
	}
	prevEpoch := layer.GetEpoch() - 1
	createActiveSet(t, o.cdb, prevEpoch.FirstLayer(), atxIDs)
	activeSet, err := o.actives(context.Background(), layer)
	require.NoError(t, err)
	require.Equal(t, createMapWithSize(numMiners-1), activeSet)
}

func TestActives_HareActiveSetMultipleLayers(t *testing.T) {
	o := defaultOracle(t)
	layer := types.NewLayerID(100)
	start, end := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	require.NotEqual(t, start, end)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	beacon := types.RandomBeacon()
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)
	numMiners := 5
	atxIDs := types.RandomActiveSet(numMiners)

	// add a miner for each layer
	extraMiners := 0
	for lyr := start; !lyr.After(end); lyr = lyr.Add(1) {
		atxIDs = append(atxIDs, types.RandomATXID())
		extraMiners++
		for _, atx := range atxIDs {
			b := types.RandomBallot()
			b.AtxID = atx
			b.RefBallot = types.EmptyBallotID
			b.EpochData = &types.EpochData{ActiveSet: atxIDs}
			b.Signature = signer.Sign(signing.HARE, b.SignedBytes())
			require.NoError(t, b.Initialize())
			b.SetSmesherID(signer.NodeID())
			require.NoError(t, ballots.Add(o.cdb, b))
		}
	}
	prevEpoch := layer.GetEpoch() - 1
	createActiveSet(t, o.cdb, prevEpoch.FirstLayer(), atxIDs)
	activeSet, err := o.actives(context.Background(), layer)
	require.NoError(t, err)
	require.Equal(t, createMapWithSize(numMiners+extraMiners), activeSet)
}

func TestActives_HareActiveSetCached(t *testing.T) {
	o := defaultOracle(t)
	numMiners := 5
	layer := types.NewLayerID(38) // manually calculated. first layer of the safe layer range
	beacon := types.RandomBeacon()
	createLayerData(t, o.cdb, layer, beacon, numMiners)
	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)

	oldActiveSet, err := o.actives(context.Background(), layer)
	require.NoError(t, err)
	require.Equal(t, createMapWithSize(numMiners), oldActiveSet)

	// should get cached hare active sets for the same safe layer range
	for lyr := layer; lyr.Before(layer.Add(defLayersPerEpoch)); lyr = lyr.Add(1) {
		weights, err := o.actives(context.Background(), lyr)
		require.NoError(t, err)
		require.Equal(t, oldActiveSet, weights)
	}

	// double the miners
	newLayer := layer.Add(defLayersPerEpoch)
	newBeacon := types.RandomBeacon()
	start, _ = safeLayerRange(newLayer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(newBeacon, nil).Times(1)
	createLayerData(t, o.cdb, newLayer, newBeacon, numMiners*2)
	newActiveSet, err := o.actives(context.Background(), layer.Add(defLayersPerEpoch))
	require.NoError(t, err)
	require.Equal(t, createMapWithSize(numMiners*2), newActiveSet)
	require.NotEqual(t, oldActiveSet, newActiveSet)
}

func TestActives_EmptyTortoiseActiveSet(t *testing.T) {
	o := defaultOracle(t)
	layer := types.NewLayerID(40)
	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(types.RandomBeacon(), nil).Times(1)
	activeSet, err := o.actives(context.Background(), layer)
	require.ErrorIs(t, err, errEmptyActiveSet)
	require.Empty(t, activeSet)
}

func TestActives_TortoiseActiveSet(t *testing.T) {
	o := defaultOracle(t)
	layer := types.NewLayerID(40)
	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(types.RandomBeacon(), nil).Times(1)

	numMiners := 5
	activeSet := types.RandomActiveSet(numMiners)
	prevEpoch := layer.GetEpoch() - 1
	for i, id := range activeSet {
		nodeID := types.BytesToNodeID([]byte(strconv.Itoa(i)))
		atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: prevEpoch.FirstLayer(),
			},
			NumUnits: uint32(i + 1),
		}}
		atx.SetID(&id)
		atx.SetNodeID(&nodeID)
		atx.SetEffectiveNumUnits(atx.NumUnits)
		atx.SetReceived(time.Now())
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		require.NoError(t, atxs.Add(o.cdb, vAtx))
	}
	oldActiveSet, err := o.actives(context.Background(), layer)
	require.NoError(t, err)
	require.Equal(t, createMapWithSize(numMiners), oldActiveSet)

	// tortoise active set is cached. it will have to be bootstrapped to guarantee consensus.
	activeSet = types.RandomActiveSet(numMiners)
	for i, id := range activeSet {
		nodeID := types.BytesToNodeID([]byte(strconv.Itoa(numMiners + i)))
		atx := &types.ActivationTx{InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: prevEpoch.FirstLayer(),
			},
			NumUnits: uint32(numMiners + i + 1),
		}}
		atx.SetID(&id)
		atx.SetNodeID(&nodeID)
		atx.SetEffectiveNumUnits(atx.NumUnits)
		atx.SetReceived(time.Now())
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		require.NoError(t, atxs.Add(o.cdb, vAtx))
	}
	newActiveSet, err := o.actives(context.Background(), layer)
	require.NoError(t, err)
	require.Equal(t, oldActiveSet, newActiveSet)
}

func TestActives_ConcurrentCalls(t *testing.T) {
	r := require.New(t)
	o := defaultOracle(t)
	layer := types.NewLayerID(100)
	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	beacon := types.RandomBeacon()
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)

	mc := NewMockcache(gomock.NewController(t))
	firstCall := true
	mc.EXPECT().Get(start.GetEpoch()).DoAndReturn(
		func(key any) (any, bool) {
			if firstCall {
				firstCall = false
				return nil, false
			}
			return createMapWithSize(5), true
		}).Times(102)
	mc.EXPECT().Add(start.GetEpoch(), gomock.Any()).Times(1)
	o.activesCache = mc
	createLayerData(t, o.cdb, layer, beacon, 5)

	var wg sync.WaitGroup
	wg.Add(102)
	runFn := func() {
		_, err := o.actives(context.Background(), types.NewLayerID(100))
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

func FuzzVrfMessageConsistency(f *testing.F) {
	tester.FuzzConsistency[VrfMessage](f)
}

func FuzzVrfMessageSafety(f *testing.F) {
	tester.FuzzSafety[VrfMessage](f)
}
