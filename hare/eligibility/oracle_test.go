package eligibility

import (
	"context"
	"errors"
	"math/rand"
	"strconv"
	"sync"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/fixed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility/config"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility/mocks"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const (
	defLayersPerEpoch uint32 = 10
	confidenceParam   uint32 = 25
	epochOffset       uint32 = 3
)

type testOracle struct {
	*Oracle
	ctrl    *gomock.Controller
	mBeacon *smocks.MockBeaconGetter
	mAtxDB  *mocks.MockatxProvider
	mMesh   *mocks.MockmeshProvider
}

func defaultOracle(t testing.TB) *testOracle {
	types.SetLayersPerEpoch(defLayersPerEpoch)
	ctrl := gomock.NewController(t)
	mb := smocks.NewMockBeaconGetter(ctrl)
	ma := mocks.NewMockatxProvider(ctrl)
	mm := mocks.NewMockmeshProvider(ctrl)
	to := &testOracle{
		Oracle:  New(mb, ma, mm, buildVerifier(true), nil, defLayersPerEpoch, config.Config{ConfidenceParam: confidenceParam, EpochOffset: epochOffset}, logtest.New(t)),
		ctrl:    ctrl,
		mBeacon: mb,
		mAtxDB:  ma,
		mMesh:   mm,
	}
	return to
}

func mockLayerBallots(tb testing.TB, to *testOracle, layer types.LayerID, beacon types.Beacon, numMiners int) {
	tb.Helper()
	activeSet := types.RandomActiveSet(numMiners)
	start, end := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	for lyr := start; !lyr.After(end); lyr = lyr.Add(1) {
		ballots := make([]*types.Ballot, 0, numMiners)
		for _, atx := range activeSet {
			b := types.RandomBallot()
			b.LayerIndex = lyr
			b.AtxID = atx
			b.RefBallot = types.EmptyBallotID
			b.EpochData = &types.EpochData{ActiveSet: activeSet, Beacon: beacon}
			b.Initialize()
			ballots = append(ballots, b)
		}
		to.mMesh.EXPECT().LayerBallots(lyr).Return(ballots, nil).Times(1)
	}
	for i, id := range activeSet {
		to.mAtxDB.EXPECT().GetAtxHeader(id).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				NodeID:    types.NodeID{Key: strconv.Itoa(i)},
				StartTick: 0,
				EndTick:   1,
			},
			NumUnits: uint(i + 1),
		}, nil).Times(1)
	}
}

func beaconWithValOne() types.Beacon {
	return types.Beacon{1, 0, 0, 0}
}

func createMapWithSize(n int) map[string]uint64 {
	m := make(map[string]uint64)
	for i := 0; i < n; i++ {
		m[strconv.Itoa(i)] = uint64(i + 1)
	}

	return m
}

func buildVerifier(result bool) verifierFunc {
	return func(pub, msg, sig []byte) bool {
		return result
	}
}

func TestCalcEligibility_ZeroCommittee(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	nid := types.NodeID{Key: "fake_node_id"}
	res, err := o.CalcEligibility(context.TODO(), types.NewLayerID(50), 1, 0, nid, []byte{})
	require.ErrorIs(t, err, errZeroCommitteeSize)
	require.Equal(t, 0, int(res))
}

func TestCalcEligibility_BeaconFailure(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	nid := types.NodeID{Key: "fake_node_id"}
	layer := types.NewLayerID(50)
	errUnknown := errors.New("unknown")
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(types.EmptyBeacon, errUnknown).Times(1)

	res, err := o.CalcEligibility(context.TODO(), layer, 0, 1, nid, []byte{})
	require.ErrorIs(t, err, errUnknown)
	require.Equal(t, 0, int(res))
}

func TestCalcEligibility_VerifyFailure(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	nid := types.NodeID{Key: "fake_node_id"}
	layer := types.NewLayerID(50)
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(types.RandomBeacon(), nil).Times(1)

	o.vrfVerifier = buildVerifier(false)
	res, err := o.CalcEligibility(context.TODO(), layer, 0, 1, nid, []byte{})
	require.NoError(t, err)
	require.Equal(t, 0, int(res))
}

func TestCalcEligibility_ZeroTotalWeight(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	nid := types.NodeID{Key: "fake_node_id"}
	layer := types.NewLayerID(40)
	start, end := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	require.Equal(t, start, end)

	beacon := types.RandomBeacon()
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(beacon, nil).Times(1)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)
	numMiners := 5
	activeSet := types.RandomActiveSet(numMiners)
	ballots := make([]*types.Ballot, 0, numMiners)
	for _, atx := range activeSet {
		b := types.RandomBallot()
		b.AtxID = atx
		b.RefBallot = types.EmptyBallotID
		b.EpochData = &types.EpochData{ActiveSet: activeSet, Beacon: beacon}
		b.Initialize()
		ballots = append(ballots, b)
	}
	o.mMesh.EXPECT().LayerBallots(start).Return(ballots, nil).AnyTimes()
	for i, id := range activeSet {
		o.mAtxDB.EXPECT().GetAtxHeader(id).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				NodeID:    types.NodeID{Key: strconv.Itoa(i)},
				StartTick: 0,
				EndTick:   1,
			},
			NumUnits: 0,
		}, nil).Times(1)
	}

	res, err := o.CalcEligibility(context.TODO(), layer, 1, 1, nid, []byte{})
	require.ErrorIs(t, err, errZeroTotalWeight)
	require.Equal(t, 0, int(res))
}

func TestCalcEligibility_EligibleFromHareActiveSet(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	layer := types.NewLayerID(50)
	beacon := beaconWithValOne()
	mockLayerBallots(t, o, layer, beacon, 5)
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(beacon, nil).Times(1)
	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)

	sigs := map[string]uint16{
		"0516a574aef37257d6811ea53ef55d4cbb0e14674900a0d5165bd6742513840d02442d979fdabc7059645d1e8f8a0f44d0db2aa90f23374dd74a3636d4ecdab7": 1,
		"73929b4b69090bb6133e2f8cd73989b35228e7e6d8c6745e4100d9c5eb48ca2624ee2889e55124195a130f74ea56e53a73a1c4dee60baa13ad3b1c0ed4f80d9c": 0,
		"e2c27ad65b752b763173b588518764b6c1e42896d57e0eabef9bcac68e07b87729a4ef9e5f17d8c1cb34ffd0d65ee9a7e63e63b77a7bcab1140a76fc04c271de": 0,
		"384460966938c87644987fe00c0f9d4f9a5e2dcd4bdc08392ed94203895ba325036725a22346e35aa707993babef716aa1b6b3dfc653a44cb23ac8f743cbbc3d": 1,
		"15c5f565a75888970059b070bfaed1998a9d423ddac9f6af83da51db02149044ea6aeb86294341c7a950ac5de2855bbebc11cc28b02c08bc903e4cf41439717d": 1,
	}
	for hex, exp := range sigs {
		sig := util.Hex2Bytes(hex)
		nid := types.NodeID{Key: "0"}
		res, err := o.CalcEligibility(context.TODO(), layer, 1, 10, nid, sig)
		require.NoError(t, err, hex)
		require.Equal(t, exp, res, hex)
	}
}

func TestCalcEligibility_EligibleFromTortoiseActiveSet(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

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
	numMiners := 5
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(beacon, nil).Times(1)
	// there is no cache for tortoise active set. so each signature will cause 2 calls to GetBeacon()
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(2 * numMiners)
	activeSet := types.RandomActiveSet(numMiners)
	o.mMesh.EXPECT().LayerBallots(start).Return([]*types.Ballot{}, nil).AnyTimes()
	// there is no cache for tortoise active set. so each signature will cause 2 calls to GetEpochAtxs() and 2*numMiners calls to GetAtxHeader()
	o.mAtxDB.EXPECT().GetEpochAtxs(layer.GetEpoch()-1).Return(activeSet, nil).Times(len(sigs) * 2)
	for i, id := range activeSet {
		o.mAtxDB.EXPECT().GetAtxHeader(id).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				NodeID:    types.NodeID{Key: strconv.Itoa(i)},
				StartTick: 0,
				EndTick:   1,
			},
			NumUnits: uint(i + 1),
		}, nil).Times(len(sigs) * 2)
	}
	for hex, exp := range sigs {
		sig := util.Hex2Bytes(hex)
		nid := types.NodeID{Key: "0"}
		res, err := o.CalcEligibility(context.TODO(), layer, 1, 10, nid, sig)
		require.NoError(t, err, hex)
		require.Equal(t, exp, res, hex)
	}
}

func TestCalcEligibility_WithSpaceUnits(t *testing.T) {
	r := require.New(t)
	numOfMiners := 50
	committeeSize := 800

	o := defaultOracle(t)
	defer o.ctrl.Finish()

	layer := types.NewLayerID(50)
	beacon := beaconWithValOne()
	mockLayerBallots(t, o, layer, beacon, numOfMiners)
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(beacon, nil).Times(1)
	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)

	var eligibilityCount uint16
	sig := make([]byte, 64)
	for pubkey := range createMapWithSize(numOfMiners) {
		n, err := rand.Read(sig)
		r.NoError(err)
		r.Equal(64, n)
		nodeID := types.NodeID{Key: pubkey}

		res, err := o.CalcEligibility(context.TODO(), layer, 1, committeeSize, nodeID, sig)
		r.NoError(err)

		valid, err := o.Validate(context.TODO(), layer, 1, committeeSize, nodeID, sig, res)
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
	defer o.ctrl.Finish()

	layer := types.NewLayerID(50)
	beacon := types.RandomBeacon()
	mockLayerBallots(t, o, layer, beacon, numOfMiners)
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(beacon, nil).Times(1)
	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)

	var eligibilityCount uint16
	sig := make([]byte, 64)
	for i := 0; i < numOfMiners; i++ {
		n, err := rng.Read(sig)
		r.NoError(err)
		r.Equal(64, n)
		nodeID := types.NodeID{Key: strconv.Itoa(i)}

		res, err := o.CalcEligibility(context.TODO(), layer, 1, committeeSize, nodeID, sig)
		r.NoError(err)

		valid, err := o.Validate(context.TODO(), layer, 1, committeeSize, nodeID, sig, res)
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
	defer o.ctrl.Finish()
	numOfMiners := 2000
	committeeSize := 800

	layer := types.NewLayerID(50)
	beacon := types.RandomBeacon()
	mockLayerBallots(b, o, layer, beacon, numOfMiners)
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(beacon, nil).Times(1)
	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)

	var eligibilityCount uint16
	sig := make([]byte, 64)

	var nodeIDs []types.NodeID
	for pubkey := range createMapWithSize(b.N) {
		nodeIDs = append(nodeIDs, types.NodeID{Key: pubkey})
	}
	b.ResetTimer()
	for _, nodeID := range nodeIDs {
		res, err := o.CalcEligibility(context.TODO(), layer, 1, committeeSize, nodeID, sig)

		if err == nil {
			valid, err := o.Validate(context.TODO(), layer, 1, committeeSize, nodeID, sig, res)
			r.NoError(err)
			r.True(valid)
		}

		eligibilityCount += res
	}
}

func Test_VrfSignVerify(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	layer := types.NewLayerID(50)
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(beaconWithValOne(), nil).Times(1)
	start, end := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	beacon := types.RandomBeacon()
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)

	numMiners := 2
	activeSet := types.RandomActiveSet(numMiners)
	for lyr := start; !lyr.After(end); lyr = lyr.Add(1) {
		ballots := make([]*types.Ballot, 0, numMiners)
		for _, atx := range activeSet {
			b := types.RandomBallot()
			b.LayerIndex = lyr
			b.AtxID = atx
			b.RefBallot = types.EmptyBallotID
			b.EpochData = &types.EpochData{ActiveSet: activeSet, Beacon: beacon}
			b.Initialize()
			ballots = append(ballots, b)
		}
		o.mMesh.EXPECT().LayerBallots(lyr).Return(ballots, nil).Times(1)
	}
	atx1 := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			NodeID:    types.NodeID{Key: "my_key"},
			StartTick: 0,
			EndTick:   1,
		},
		NumUnits: 1 * 1024,
	}
	atx2 := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			NodeID:    types.NodeID{Key: "abc"},
			StartTick: 0,
			EndTick:   1,
		},
		NumUnits: 9 * 1024,
	}
	o.mAtxDB.EXPECT().GetAtxHeader(activeSet[0]).Return(atx1, nil).Times(1)
	o.mAtxDB.EXPECT().GetAtxHeader(activeSet[1]).Return(atx2, nil).Times(1)

	o.vrfVerifier = signing.VRFVerify
	seed := make([]byte, 32)
	// eligibility of the proof depends on the identity (private key is generated from seed)
	// all zeroes doesn't pass the test
	seed[0] = 1
	vrfSigner, vrfPubkey, err := signing.NewVRFSigner(seed)
	require.NoError(t, err)

	o.vrfSigner = vrfSigner
	id := types.NodeID{Key: "my_key", VRFPublicKey: vrfPubkey}
	proof, err := o.Proof(context.TODO(), layer, 1)
	assert.NoError(t, err)

	res, err := o.CalcEligibility(context.TODO(), layer, 1, 10, id, proof)
	assert.NoError(t, err)
	assert.Equal(t, uint16(1), res)

	valid, err := o.Validate(context.TODO(), layer, 1, 10, id, proof, 1)
	assert.NoError(t, err)
	assert.True(t, valid)
}

func Test_Proof_BeaconError(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	layer := types.NewLayerID(2)
	errUnknown := errors.New("unknown")
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(types.EmptyBeacon, errUnknown).Times(1)

	sig, err := o.Proof(context.TODO(), layer, 3)
	assert.Nil(t, sig)
	assert.ErrorIs(t, err, errUnknown)
}

func Test_Proof(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	layer := types.NewLayerID(2)
	o.mBeacon.EXPECT().GetBeacon(layer.GetEpoch()).Return(beaconWithValOne(), nil).Times(1)

	signer := signing.NewEdSigner()
	edPubkey := signer.PublicKey()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(edPubkey.Bytes()))
	require.NoError(t, err)

	o.vrfSigner = vrfSigner
	sig, err := o.Proof(context.TODO(), layer, 3)
	assert.Nil(t, err)
	assert.NotNil(t, sig)
}

func TestOracle_IsIdentityActive(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	layer := types.NewLayerID(40)
	start, end := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	require.Equal(t, start, end)

	beacon := types.RandomBeacon()
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)

	numMiners := 2
	activeSet := types.RandomActiveSet(numMiners)
	ballots := make([]*types.Ballot, 0, numMiners)
	for _, atx := range activeSet {
		b := types.RandomBallot()
		b.LayerIndex = start
		b.AtxID = atx
		b.RefBallot = types.EmptyBallotID
		b.EpochData = &types.EpochData{ActiveSet: activeSet, Beacon: beacon}
		b.Initialize()
		ballots = append(ballots, b)
	}
	o.mMesh.EXPECT().LayerBallots(start).Return(ballots, nil).Times(1)
	atx1 := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			NodeID:    types.NodeID{Key: "11111"},
			StartTick: 0,
			EndTick:   1,
		},
		NumUnits: 1 * 1024,
	}
	atx2 := &types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			NodeID:    types.NodeID{Key: "22222"},
			StartTick: 0,
			EndTick:   1,
		},
		NumUnits: 9 * 1024,
	}
	o.mAtxDB.EXPECT().GetAtxHeader(activeSet[0]).Return(atx1, nil).Times(1)
	o.mAtxDB.EXPECT().GetAtxHeader(activeSet[1]).Return(atx2, nil).Times(1)

	for _, edID := range []string{"11111", "22222"} {
		v, err := o.IsIdentityActiveOnConsensusView(context.TODO(), edID, layer)
		require.NoError(t, err)
		assert.True(t, v)
	}
	v, err := o.IsIdentityActiveOnConsensusView(context.TODO(), "smesher", layer)
	require.NoError(t, err)
	assert.False(t, v)
}

func TestBuildVRFMessage_BeaconError(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	errUnknown := errors.New("unknown")
	o.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.EmptyBeacon, errUnknown).Times(1)
	msg, err := o.buildVRFMessage(context.TODO(), types.NewLayerID(1), 1)
	assert.ErrorIs(t, err, errUnknown)
	assert.Nil(t, msg)
}

func TestBuildVRFMessage(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	firstLayer := types.NewLayerID(1)
	secondLayer := firstLayer.Add(1)
	beacon := types.RandomBeacon()
	o.mBeacon.EXPECT().GetBeacon(firstLayer.GetEpoch()).Return(beacon, nil).Times(1)
	m1, err := o.buildVRFMessage(context.TODO(), firstLayer, 2)
	require.NoError(t, err)
	m2, ok := o.vrfMsgCache.Get(buildKey(firstLayer, 2))
	require.True(t, ok)
	assert.Equal(t, m1, m2) // check same as in cache

	// check not same for different round
	o.mBeacon.EXPECT().GetBeacon(firstLayer.GetEpoch()).Return(beacon, nil).Times(1)
	m3, err := o.buildVRFMessage(context.TODO(), firstLayer, 3)
	require.NoError(t, err)
	assert.NotEqual(t, m1, m3)

	// check not same for different layer
	o.mBeacon.EXPECT().GetBeacon(firstLayer.GetEpoch()).Return(beacon, nil).Times(1)
	m4, err := o.buildVRFMessage(context.TODO(), secondLayer, 2)
	require.NoError(t, err)
	assert.NotEqual(t, m1, m4)

	// even tho beacon value changed, we will get the same cached VRF message
	o.mBeacon.EXPECT().GetBeacon(firstLayer.GetEpoch()).Return(types.RandomBeacon(), nil).Times(0)
	m5, err := o.buildVRFMessage(context.TODO(), firstLayer, 2)
	require.NoError(t, err)
	assert.Equal(t, m1, m5) // check same result (from cache)
}

func TestBuildVRFMessage_Concurrency(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	mCache := mocks.NewMockcache(o.ctrl)
	o.vrfMsgCache = mCache

	total := 1000
	expectAdd := 10
	wg := sync.WaitGroup{}
	firstLayer := types.NewLayerID(1)
	o.mBeacon.EXPECT().GetBeacon(firstLayer.GetEpoch()).Return(types.RandomBeacon(), nil).AnyTimes()
	for i := 0; i < expectAdd; i++ {
		key := buildKey(firstLayer, uint32(i))
		mCache.EXPECT().Add(key, gomock.Any()).Return(false).Times(1)
		mCache.EXPECT().Get(key).Return(nil, false).Times(1)
	}
	mCache.EXPECT().Get(gomock.Any()).Return(types.RandomBytes(100), true).Times(total - expectAdd)
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(x int) {
			_, err := o.buildVRFMessage(context.TODO(), firstLayer, uint32(x%expectAdd))
			require.NoError(t, err)
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
		assert.Equal(t, testCase.safeLayerStart, sls, "got incorrect safeLayerStart")
		assert.Equal(t, testCase.safeLayerEnd, sle, "got incorrect safeLayerEnd")
	}
}

func TestActives_GetLayerBallotsError(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	layer := types.NewLayerID(50)
	beacon := types.RandomBeacon()
	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)

	errUnknown := errors.New("unknown")
	o.mMesh.EXPECT().LayerBallots(gomock.Any()).Return(nil, errUnknown).Times(1)

	activeSet, err := o.actives(context.TODO(), layer)
	assert.ErrorIs(t, err, errUnknown)
	assert.Empty(t, activeSet)
}

func TestActives_HareActiveSet(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	numMiners := 5
	layer := types.NewLayerID(50)
	beacon := types.RandomBeacon()
	mockLayerBallots(t, o, layer, beacon, numMiners)
	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)

	activeSet, err := o.actives(context.TODO(), layer)
	require.NoError(t, err)
	assert.Equal(t, createMapWithSize(numMiners), activeSet)
}

func TestActives_HareActiveSetDifferentBeacon(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	layer := types.NewLayerID(50)
	start, end := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	beacon := types.RandomBeacon()
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)
	numMiners := 5
	atxIDs := types.RandomActiveSet(numMiners)
	badBeacon := types.RandomBeacon()
	badBeaconATX := atxIDs[len(atxIDs)-1]
	for lyr := start; !lyr.After(end); lyr = lyr.Add(1) {
		ballots := make([]*types.Ballot, 0, numMiners)
		for _, atx := range atxIDs {
			b := types.RandomBallot()
			b.LayerIndex = lyr
			b.AtxID = atx
			b.RefBallot = types.EmptyBallotID
			if atx == badBeaconATX {
				b.EpochData = &types.EpochData{ActiveSet: atxIDs, Beacon: badBeacon}
			} else {
				b.EpochData = &types.EpochData{ActiveSet: atxIDs, Beacon: beacon}
			}
			b.Initialize()
			ballots = append(ballots, b)
		}
		o.mMesh.EXPECT().LayerBallots(lyr).Return(ballots, nil).Times(1)
	}
	for i, id := range atxIDs {
		o.mAtxDB.EXPECT().GetAtxHeader(id).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				NodeID:    types.NodeID{Key: strconv.Itoa(i)},
				StartTick: 0,
				EndTick:   1,
			},
			NumUnits: uint(i + 1),
		}, nil).AnyTimes()
	}
	activeSet, err := o.actives(context.TODO(), layer)
	require.NoError(t, err)
	assert.Equal(t, createMapWithSize(numMiners-1), activeSet)
}

func TestActives_HareActiveSetMultipleLayers(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	layer := types.NewLayerID(100)
	start, end := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	require.NotEqual(t, start, end)
	beacon := types.RandomBeacon()
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)
	numMiners := 5
	atxIDs := types.RandomActiveSet(numMiners)

	// add a miner for each layer
	extraMiners := 0
	for lyr := start; !lyr.After(end); lyr = lyr.Add(1) {
		atxIDs = append(atxIDs, types.RandomATXID())
		extraMiners++
		ballots := make([]*types.Ballot, 0, numMiners)
		for _, atx := range atxIDs {
			b := types.RandomBallot()
			b.AtxID = atx
			b.RefBallot = types.EmptyBallotID
			b.EpochData = &types.EpochData{ActiveSet: atxIDs}
			b.Initialize()
			ballots = append(ballots, b)
		}
		o.mMesh.EXPECT().LayerBallots(lyr).Return(ballots, nil).Times(1)
	}
	for i, id := range atxIDs {
		o.mAtxDB.EXPECT().GetAtxHeader(id).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				NodeID:    types.NodeID{Key: strconv.Itoa(i)},
				StartTick: 0,
				EndTick:   1,
			},
			NumUnits: uint(i + 1),
		}, nil).Times(1)
	}
	activeSet, err := o.actives(context.TODO(), layer)
	require.NoError(t, err)
	assert.Equal(t, createMapWithSize(numMiners+extraMiners), activeSet)
}

func TestActives_HareActiveSetCached(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	numMiners := 5
	layer := types.NewLayerID(38) // manually calculated. first layer of the safe layer range
	beacon := types.RandomBeacon()
	mockLayerBallots(t, o, layer, beacon, numMiners)
	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)

	oldActiveSet, err := o.actives(context.TODO(), layer)
	require.NoError(t, err)
	assert.Equal(t, createMapWithSize(numMiners), oldActiveSet)

	// should get cached hare active sets for the same safe layer range
	for lyr := layer; lyr.Before(layer.Add(defLayersPerEpoch)); lyr = lyr.Add(1) {
		weights, err := o.actives(context.TODO(), lyr)
		require.NoError(t, err)
		assert.Equal(t, oldActiveSet, weights)
	}

	// double the miners
	newLayer := layer.Add(defLayersPerEpoch)
	newBeacon := types.RandomBeacon()
	start, _ = safeLayerRange(newLayer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(newBeacon, nil).Times(1)
	mockLayerBallots(t, o, newLayer, newBeacon, numMiners*2)
	newActiveSet, err := o.actives(context.TODO(), layer.Add(defLayersPerEpoch))
	require.NoError(t, err)
	assert.Equal(t, createMapWithSize(numMiners*2), newActiveSet)
	assert.NotEqual(t, oldActiveSet, newActiveSet)
}

func TestActives_GetEpochATXsError(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	layer := types.NewLayerID(40)
	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(types.RandomBeacon(), nil).Times(1)

	o.mMesh.EXPECT().LayerBallots(gomock.Any()).Return([]*types.Ballot{}, nil).Times(1)
	errUnknown := errors.New("unknown")
	o.mAtxDB.EXPECT().GetEpochAtxs(gomock.Any()).Return(nil, errUnknown).Times(1)

	weights, err := o.actives(context.TODO(), layer)
	assert.ErrorIs(t, err, errUnknown)
	assert.Empty(t, weights)
}

func TestActives_EmptyTortoiseActiveSet(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	layer := types.NewLayerID(40)
	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(types.RandomBeacon(), nil).Times(1)

	o.mMesh.EXPECT().LayerBallots(gomock.Any()).Return([]*types.Ballot{}, nil).Times(1)
	o.mAtxDB.EXPECT().GetEpochAtxs(gomock.Any()).Return([]types.ATXID{}, nil).Times(1)

	activeSet, err := o.actives(context.TODO(), layer)
	assert.ErrorIs(t, err, errEmptyActiveSet)
	assert.Empty(t, activeSet)
}

func TestActives_TortoiseActiveSet(t *testing.T) {
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	layer := types.NewLayerID(40)
	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(types.RandomBeacon(), nil).Times(2)

	numMiners := 5
	activeSet := types.RandomActiveSet(numMiners)
	o.mMesh.EXPECT().LayerBallots(gomock.Any()).Return([]*types.Ballot{}, nil).Times(1)
	o.mAtxDB.EXPECT().GetEpochAtxs(gomock.Any()).Return(activeSet, nil).Times(1)
	for i, id := range activeSet {
		o.mAtxDB.EXPECT().GetAtxHeader(id).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				NodeID:    types.NodeID{Key: strconv.Itoa(i)},
				StartTick: 0,
				EndTick:   1,
			},
			NumUnits: uint(i + 1),
		}, nil).Times(1)
	}
	oldActiveSet, err := o.actives(context.TODO(), layer)
	assert.NoError(t, err)
	assert.Equal(t, createMapWithSize(numMiners), oldActiveSet)

	// tortoise active set is not cached. same layer may yield different answer
	activeSet = types.RandomActiveSet(numMiners * 2)
	o.mMesh.EXPECT().LayerBallots(gomock.Any()).Return([]*types.Ballot{}, nil).Times(1)
	o.mAtxDB.EXPECT().GetEpochAtxs(gomock.Any()).Return(activeSet, nil).Times(1)
	for i, id := range activeSet {
		o.mAtxDB.EXPECT().GetAtxHeader(id).Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				NodeID:    types.NodeID{Key: strconv.Itoa(i)},
				StartTick: 0,
				EndTick:   1,
			},
			NumUnits: uint(i + 1),
		}, nil).Times(1)
	}
	newActiveSet, err := o.actives(context.TODO(), layer)
	assert.NoError(t, err)
	assert.Equal(t, createMapWithSize(numMiners*2), newActiveSet)
	assert.NotEqual(t, oldActiveSet, newActiveSet)
}

func TestActives_ConcurrentCalls(t *testing.T) {
	r := require.New(t)
	o := defaultOracle(t)
	defer o.ctrl.Finish()

	layer := types.NewLayerID(100)
	start, _ := safeLayerRange(layer, confidenceParam, defLayersPerEpoch, epochOffset)
	beacon := types.RandomBeacon()
	o.mBeacon.EXPECT().GetBeacon(start.GetEpoch()).Return(beacon, nil).Times(1)

	mc := mocks.NewMockcache(o.ctrl)
	firstCall := true
	mc.EXPECT().Get(start.GetEpoch()).DoAndReturn(
		func(key interface{}) (interface{}, bool) {
			if firstCall {
				firstCall = false
				return nil, false
			}
			return createMapWithSize(5), true
		}).Times(102)
	mc.EXPECT().Add(start.GetEpoch(), gomock.Any()).Times(1)
	o.activesCache = mc
	mockLayerBallots(t, o, layer, beacon, 5)

	var wg sync.WaitGroup
	wg.Add(102)
	runFn := func() {
		_, err := o.actives(context.TODO(), types.NewLayerID(100))
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

func TestEncodeBeacon(t *testing.T) {
	beacon := types.HexToBeacon("0xaeebad4a796fcc2e15dc4c6061b45ed9b373f26adfc798ca7d2d8cc58182718e")
	expected := uint32(0x4aadebae)
	assert.Equal(t, expected, encodeBeacon(beacon))
}
