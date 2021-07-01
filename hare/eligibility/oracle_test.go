package eligibility

import (
	"context"
	"errors"
	"fmt"
	lru "github.com/hashicorp/golang-lru"
	"github.com/spacemeshos/fixed"
	"github.com/spacemeshos/go-spacemesh/common/types"
	eCfg "github.com/spacemeshos/go-spacemesh/hare/eligibility/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math/rand"
	"strconv"
	"sync"
	"testing"
)

const defSafety = types.LayerID(35)
const defLayersPerEpoch = 10

var errFoo = errors.New("some error")
var errMy = errors.New("my error")
var cfg = eCfg.Config{ConfidenceParam: 25, EpochOffset: 3}

var (
	genWeight          = 5
	spacePerUnit       = 1
	genesisMinerWeight = 1
)

type mockBlocksProvider struct {
	mp                             map[types.BlockID]struct{}
	layerContextuallyValidBlocksFn func(types.LayerID) (map[types.BlockID]struct{}, error)
}

func (mbp mockBlocksProvider) LayerContextuallyValidBlocks(layerID types.LayerID) (map[types.BlockID]struct{}, error) {
	if mbp.layerContextuallyValidBlocksFn != nil {
		return mbp.layerContextuallyValidBlocksFn(layerID)
	}
	if mbp.mp == nil {
		mbp.mp = make(map[types.BlockID]struct{})
		block1 := types.NewExistingBlock(0, []byte("some data"), nil)
		mbp.mp[block1.ID()] = struct{}{}
	}
	return mbp.mp, nil
}

type mockValueProvider struct {
	val uint32
	err error
}

func (mvp *mockValueProvider) Value(context.Context, types.EpochID) (uint32, error) {
	return mvp.val, mvp.err
}

type mockActiveSetProvider struct {
	size           int
	getActiveSetFn func(types.EpochID, map[types.BlockID]struct{}) (map[string]uint64, error)
	getEpochAtxsFn func(types.EpochID) []types.ATXID
	getAtxHeaderFn func(types.ATXID) (*types.ActivationTxHeader, error)
}

func (m *mockActiveSetProvider) GetMinerWeightsInEpochFromView(targetEpoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]uint64, error) {
	if m.getActiveSetFn != nil {
		return m.getActiveSetFn(targetEpoch, blocks)
	}
	return createMapWithSize(m.size), nil
}

func (m *mockActiveSetProvider) GetEpochAtxs(epoch types.EpochID) []types.ATXID {
	if m.getEpochAtxsFn != nil {
		return m.getEpochAtxsFn(epoch)
	}
	atxList := make([]types.ATXID, m.size)
	for i := 0; i < m.size; i++ {
		atxList[i] = types.ATXID{}
	}
	return atxList
}

func (m *mockActiveSetProvider) GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error) {
	if m.getAtxHeaderFn != nil {
		return m.getAtxHeaderFn(id)
	}
	return &types.ActivationTxHeader{NIPSTChallenge: types.NIPSTChallenge{NodeID: types.NodeID{Key: "fakekey"}}}, nil
}

type mockBufferedActiveSetProvider struct {
	size map[types.EpochID]int
}

func (m *mockBufferedActiveSetProvider) GetMinerWeightsInEpochFromView(targetEpoch types.EpochID, _ map[types.BlockID]struct{}) (map[string]uint64, error) {
	v, ok := m.size[targetEpoch]
	if !ok {
		return createMapWithSize(0), fmt.Errorf("no size for epoch %v", targetEpoch)
	}

	return createMapWithSize(v), nil
}

func (m *mockBufferedActiveSetProvider) GetEpochAtxs(types.EpochID) []types.ATXID {
	panic("not implemented")
}

func (m *mockBufferedActiveSetProvider) GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error) {
	panic("not implemented")
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

type mockSigner struct {
	sig []byte
}

func (s *mockSigner) Sign([]byte) []byte {
	return s.sig
}

type mockCacher struct {
	data   map[interface{}]interface{}
	numAdd int
	numGet int
	mutex  sync.Mutex
}

func newMockCacher() *mockCacher {
	return &mockCacher{make(map[interface{}]interface{}), 0, 0, sync.Mutex{}}
}

func (mc *mockCacher) Add(key, value interface{}) (evicted bool) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()

	_, evicted = mc.data[key]
	mc.data[key] = value
	mc.numAdd++
	return evicted
}

func (mc *mockCacher) Get(key interface{}) (value interface{}, ok bool) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()

	v, ok := mc.data[key]
	mc.numGet++
	return v, ok
}

func TestOracle_BuildVRFMessage(t *testing.T) {
	r := require.New(t)
	types.SetLayersPerEpoch(defLayersPerEpoch)
	o := Oracle{vrfMsgCache: newMockCacher(), Log: log.NewDefault(t.Name())}
	o.beacon = &mockValueProvider{1, errFoo}
	_, err := o.buildVRFMessage(context.TODO(), types.LayerID(1), 1)
	r.Equal(errFoo, err)

	o.beacon = &mockValueProvider{1, nil}
	m, err := o.buildVRFMessage(context.TODO(), 1, 2)
	r.NoError(err)
	m2, ok := o.vrfMsgCache.Get(buildKey(1, 2))
	r.True(ok)
	r.Equal(m, m2) // check same as in cache

	// check not same for different round
	m4, err := o.buildVRFMessage(context.TODO(), 1, 3)
	r.NoError(err)
	r.NotEqual(m, m4)

	// check not same for different layer
	m5, err := o.buildVRFMessage(context.TODO(), 2, 2)
	r.NoError(err)
	r.NotEqual(m, m5)

	o.beacon = &mockValueProvider{5, nil} // set different value
	m3, err := o.buildVRFMessage(context.TODO(), 1, 2)
	r.NoError(err)
	r.Equal(m, m3) // check same result (from cache)
}

func TestOracle_buildVRFMessageConcurrency(t *testing.T) {
	r := require.New(t)
	o := New(&mockValueProvider{1, nil}, &mockActiveSetProvider{size: 10}, &mockBlocksProvider{}, buildVerifier(true), &mockSigner{[]byte{1, 2, 3}}, 5, 1, 5, 1, cfg, log.NewDefault(t.Name()))
	mCache := newMockCacher()
	o.vrfMsgCache = mCache

	total := 1000
	wg := sync.WaitGroup{}
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func(x int) {
			_, err := o.buildVRFMessage(context.TODO(), 1, int32(x%10))
			r.NoError(err)
			wg.Done()
		}(i)
	}

	wg.Wait()
	r.Equal(10, mCache.numAdd)
}

func TestOracle_IsEligible(t *testing.T) {
	nid := types.NodeID{Key: "fake_node_id"}
	o := defaultOracle(t)
	o.layersPerEpoch = 10

	// VRF is ineligible
	o.vrfVerifier = buildVerifier(false)
	res, err := o.CalcEligibility(context.TODO(), types.LayerID(1), 0, 1, nid, []byte{})
	require.NoError(t, err)
	require.Equal(t, 0, int(res))

	// VRF eligible but committee size zero
	o.vrfVerifier = buildVerifier(true)
	res, err = o.CalcEligibility(context.TODO(), types.LayerID(50), 1, 0, nid, []byte{})
	require.NoError(t, err)
	require.Equal(t, 0, int(res))

	// VRF eligible but empty active set
	o.atxdb = &mockActiveSetProvider{size: 0}
	// need to run on a layerID in a different epoch to avoid the cached value
	res, err = o.CalcEligibility(context.TODO(), types.LayerID(cfg.ConfidenceParam*2+11), 1, 1, nid, []byte{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty active set")
	require.Equal(t, 0, int(res))

	// eligible
	o.atxdb = &mockActiveSetProvider{size: 5}
	sig := make([]byte, 64)
	for pubkey := range createMapWithSize(1) {
		n, err := rand.Read(sig)
		require.NoError(t, err)
		require.Equal(t, 64, n)
		nid := types.NodeID{Key: pubkey}
		res, err = o.CalcEligibility(context.TODO(), types.LayerID(50), 1, 10, nid, sig)
		require.NoError(t, err)
		require.Greater(t, int(res), 0)
	}
}

func Test_SafeLayerRange(t *testing.T) {
	types.SetLayersPerEpoch(defLayersPerEpoch)
	safetyParam := uint16(10)
	effGenesis := types.GetEffectiveGenesis()
	testCases := []struct {
		// input
		targetLayer    types.LayerID
		safetyParam    uint16
		layersPerEpoch uint16
		epochOffset    uint16
		// expected output
		safeLayerStart types.LayerID
		safeLayerEnd   types.LayerID
	}{
		// a target layer prior to effective genesis returns effective genesis
		{0, safetyParam, defLayersPerEpoch, 1, effGenesis, effGenesis},
		// large safety param also returns effective genesis
		{100, 100, defLayersPerEpoch, 1, effGenesis, effGenesis},
		// safe layer in first safetyParam + epochOffset layers of epoch, rewinds one epoch further (two prior to target)
		{100, safetyParam, defLayersPerEpoch, 1, 80, 81},
		// zero offset
		{100, safetyParam, defLayersPerEpoch, 0, 90, 90},
		// safetyParam < layersPerEpoch means only look back one epoch
		{100, safetyParam - 1, defLayersPerEpoch, 1, 90, 91},
		// larger epochOffset looks back further
		{100, safetyParam, defLayersPerEpoch, 5, 80, 85},
		// smaller safety param returns one epoch prior
		{100, 5, defLayersPerEpoch, 5, 90, 95},
		// targetLayer within safetyParam returns one epoch prior
		{105, 5, defLayersPerEpoch, 5, 90, 95},
		// targetLayer after safetyParam+epochOffset returns start of same epoch
		{105, 2, defLayersPerEpoch, 2, 100, 102},
	}
	for _, testCase := range testCases {
		log.Info("testing safeLayerRange input: %v", testCase)
		sls, sle := safeLayerRange(testCase.targetLayer, testCase.safetyParam, testCase.layersPerEpoch, testCase.epochOffset)
		assert.Equal(t, testCase.safeLayerStart, sls, "got incorrect safeLayerStart")
		assert.Equal(t, testCase.safeLayerEnd, sle, "got incorrect safeLayerEnd")
	}
}

func defaultOracle(t testing.TB) *Oracle {
	layersPerEpoch := uint16(defLayersPerEpoch)
	return mockOracle(t, layersPerEpoch)
}

func mockOracle(t testing.TB, layersPerEpoch uint16) *Oracle {
	types.SetLayersPerEpoch(int32(layersPerEpoch))
	o := New(&mockValueProvider{1, nil}, &mockActiveSetProvider{}, &mockBlocksProvider{}, buildVerifier(true), nil, layersPerEpoch, uint64(spacePerUnit), uint64(genWeight), uint64(genesisMinerWeight), cfg, log.NewDefault(t.Name()))
	return o
}

func TestOracle_CalcEligibility_ErrorFromVerifier(t *testing.T) {
	r := require.New(t)
	o := defaultOracle(t)
	o.vrfVerifier = buildVerifier(false)

	res, err := o.CalcEligibility(context.TODO(), types.LayerID(1), 0, 1, types.NodeID{}, []byte{})

	r.NoError(err)
	r.Equal(uint16(0), res)
}

func TestOracle_CalcEligibility_ErrorFromBeacon(t *testing.T) {
	r := require.New(t)
	o := defaultOracle(t)
	o.beacon = &mockValueProvider{0, errFoo}

	res, err := o.CalcEligibility(context.TODO(), types.LayerID(1), 0, 1, types.NodeID{}, []byte{})

	r.EqualError(err, errFoo.Error())
	r.Equal(uint16(0), res)
}

func TestOracle_CalcEligibility_ErrorFromActiveSet(t *testing.T) {
	r := require.New(t)
	o := defaultOracle(t)
	o.atxdb = &mockActiveSetProvider{getActiveSetFn: func(epoch types.EpochID, view map[types.BlockID]struct{}) (map[string]uint64, error) {
		return nil, errFoo
	}}

	res, err := o.CalcEligibility(context.TODO(), types.LayerID(50), 0, 1, types.NodeID{}, []byte{})

	r.Error(err)
	r.Contains(err.Error(), errFoo.Error())
	r.Equal(uint16(0), res)
}

func TestOracle_CalcEligibility_ZeroTotalWeight(t *testing.T) {
	r := require.New(t)
	o := defaultOracle(t)
	o.atxdb = &mockActiveSetProvider{getActiveSetFn: func(epoch types.EpochID, view map[types.BlockID]struct{}) (map[string]uint64, error) {
		for bid := range view {
			return map[string]uint64{
				bid.String(): 0,
			}, nil
		}
		return nil, nil
	}}

	res, err := o.CalcEligibility(context.TODO(), types.LayerID(cfg.ConfidenceParam*2+11), 0, 1, types.NodeID{}, []byte{})

	r.EqualError(err, "total weight is zero")
	r.Equal(uint16(0), res)
}

func TestOracle_CalcEligibility_ZeroCommitteeSize(t *testing.T) {
	r := require.New(t)
	o := defaultOracle(t)
	o.atxdb = &mockActiveSetProvider{size: 10}

	nodeID := types.NodeID{VRFPublicKey: []byte(strconv.Itoa(0))}
	sig := make([]byte, 64)
	for i := 0; i < 1000; i++ {
		n, err := rand.Read(sig)
		r.NoError(err)
		r.Equal(64, n)

		res, err := o.CalcEligibility(context.TODO(), types.LayerID(cfg.ConfidenceParam+11), 0, 0, nodeID, sig)

		r.NoError(err)
		r.Equal(uint16(0), res)
	}
}

func TestOracle_CalcEligibility(t *testing.T) {
	r := require.New(t)
	numOfMiners := 2000
	committeeSize := 800
	o := defaultOracle(t)
	o.atxdb = &mockActiveSetProvider{size: numOfMiners}

	var eligibilityCount uint16
	sig := make([]byte, 64)
	for pubkey := range createMapWithSize(numOfMiners) {
		n, err := rand.Read(sig)
		r.NoError(err)
		r.Equal(64, n)
		nodeID := types.NodeID{Key: pubkey}

		res, err := o.CalcEligibility(context.TODO(), types.LayerID(50), 1, committeeSize, nodeID, sig)
		r.NoError(err)

		valid, err := o.Validate(context.TODO(), types.LayerID(50), 1, committeeSize, nodeID, sig, res)
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
	o.atxdb = &mockActiveSetProvider{size: numOfMiners}

	var eligibilityCount uint16
	sig := make([]byte, 64)

	var nodeIDs []types.NodeID
	for pubkey := range createMapWithSize(b.N) {
		nodeIDs = append(nodeIDs, types.NodeID{Key: pubkey})
	}
	b.ResetTimer()
	for _, nodeID := range nodeIDs {
		res, err := o.CalcEligibility(context.TODO(), types.LayerID(50), 1, committeeSize, nodeID, sig)

		if err == nil {
			valid, err := o.Validate(context.TODO(), types.LayerID(50), 1, committeeSize, nodeID, sig, res)
			r.NoError(err)
			r.True(valid)
		}

		eligibilityCount += res
	}
}

func Test_ActiveSetSize(t *testing.T) {
	m := make(map[types.EpochID]int)
	// these epochs correspond to the safe layer start epochs for the input layers
	m[types.EpochID(4)] = 2
	m[types.EpochID(5)] = 3
	m[types.EpochID(6)] = 5
	o := defaultOracle(t)
	o.atxdb = &mockBufferedActiveSetProvider{size: m}
	l := 39 + defSafety
	assertActiveSetSize(t, o, epochWeight(2), l)
	assertActiveSetSize(t, o, epochWeight(3), l+10)
	assertActiveSetSize(t, o, epochWeight(5), l+20)

	// create error
	o.activesCache, _ = lru.New(activesCacheSize)
	o.atxdb = &mockActiveSetProvider{getActiveSetFn: func(epoch types.EpochID, view map[types.BlockID]struct{}) (map[string]uint64, error) {
		return createMapWithSize(5), errors.New("fake err")
	}}
	activeSetSize, err := o.totalWeight(context.TODO(), l+19)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "fake err")
	assert.Equal(t, uint64(0), activeSetSize)
}

func epochWeight(numMiners int) uint64 {
	m := createMapWithSize(numMiners)
	totalWeight := uint64(0)
	for _, w := range m {
		totalWeight += w
	}
	return totalWeight
}

func assertActiveSetSize(t *testing.T, o *Oracle, expected uint64, l types.LayerID) {
	activeSetSize, err := o.totalWeight(context.TODO(), l)
	assert.NoError(t, err)
	assert.Equal(t, int(expected), int(activeSetSize))
}

func Test_VrfSignVerify(t *testing.T) {
	o := defaultOracle(t)
	o.atxdb = &mockActiveSetProvider{getActiveSetFn: func(types.EpochID, map[types.BlockID]struct{}) (map[string]uint64, error) {
		return map[string]uint64{
			"my_key": 1 * 1024,
			"abc":    9 * 1024,
		}, nil
	}}
	o.vrfVerifier = signing.VRFVerify
	seed := make([]byte, 32)
	//rand.Read(seed)
	vrfSigner, vrfPubkey, err := signing.NewVRFSigner(seed)
	assert.NoError(t, err)
	o.vrfSigner = vrfSigner
	id := types.NodeID{Key: "my_key", VRFPublicKey: vrfPubkey}
	proof, err := o.Proof(context.TODO(), 50, 1)
	assert.NoError(t, err)

	res, err := o.CalcEligibility(context.TODO(), 50, 1, 10, id, proof)
	assert.NoError(t, err)
	assert.Equal(t, uint16(1), res)

	valid, err := o.Validate(context.TODO(), 50, 1, 10, id, proof, 1)
	assert.NoError(t, err)
	assert.True(t, valid)
}

func TestOracle_Proof(t *testing.T) {
	o := defaultOracle(t)
	o.beacon = &mockValueProvider{0, errMy}
	sig, err := o.Proof(context.TODO(), 2, 3)
	assert.Nil(t, sig)
	assert.EqualError(t, err, errMy.Error())

	o.beacon = &mockValueProvider{0, nil}
	o.vrfSigner = &mockSigner{nil}
	sig, err = o.Proof(context.TODO(), 2, 3)
	assert.Nil(t, sig)
	assert.NoError(t, err)
	mySig := []byte{1, 2}
	o.vrfSigner = &mockSigner{mySig}
	sig, err = o.Proof(context.TODO(), 2, 3)
	assert.Nil(t, err)
	assert.Equal(t, mySig, sig)
}

func TestOracle_activeSetSizeCache(t *testing.T) {
	r := require.New(t)
	o := defaultOracle(t)
	o.atxdb = &mockActiveSetProvider{getActiveSetFn: func(epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]uint64, error) {
		return createMapWithSize(17), nil
	}}
	v1, e := o.totalWeight(context.TODO(), defSafety+100)
	r.NoError(e)

	o.atxdb = &mockActiveSetProvider{getActiveSetFn: func(epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]uint64, error) {
		return createMapWithSize(19), nil
	}}
	v2, e := o.totalWeight(context.TODO(), defSafety+100)
	r.NoError(e)
	r.Equal(v1, v2)
}

func TestOracle_actives(t *testing.T) {
	r := require.New(t)
	numAtxs := 9
	atxdb := &mockActiveSetProvider{
		size: 1,
		getEpochAtxsFn: func(types.EpochID) []types.ATXID {
			atxids := make([]types.ATXID, numAtxs)
			for i := 0; i < numAtxs; i++ {
				atxids[i] = types.ATXID(types.BytesToHash([]byte{byte(i)}))
			}
			return atxids
		},
		getAtxHeaderFn: func(atxid types.ATXID) (*types.ActivationTxHeader, error) {
			return &types.ActivationTxHeader{
				NIPSTChallenge: types.NIPSTChallenge{NodeID: types.NodeID{Key: atxid.Hash32().String()}},
			}, nil
		},
	}

	o := defaultOracle(t)
	o.atxdb = atxdb
	t.Run("test genesis", func(t *testing.T) {
		_, err := o.actives(context.TODO(), 1)
		r.EqualError(err, errGenesis.Error())
	})

	t.Run("test cache", func(t *testing.T) {
		mc := newMockCacher()
		o.activesCache = mc
		v1, err := o.actives(context.TODO(), 100)
		r.NoError(err)
		v2, err := o.actives(context.TODO(), 100)
		r.NoError(err)
		r.Equal(v1, v2)
		r.Equal(1, mc.numAdd)
		r.Equal(2, mc.numGet) // one hit, one miss
	})

	t.Run("test tortoise active set", func(t *testing.T) {
		// clear the cache
		o.activesCache = newMockCacher()
		// return no hare active set, forcing us to fall back on tortoise active set
		atxdb.size = 0
		v1, err := o.actives(context.TODO(), 100)
		r.NoError(err)
		for i := 0; i < numAtxs; i++ {
			// this works too:
			//atxid := types.ATXID(types.BytesToHash([]byte{byte(i)}))
			//r.Contains(v1, atxid.Hash32().String())
			r.Contains(v1, fmt.Sprintf("0x%064x", i))
		}
	})

	t.Run("test cache key", func(t *testing.T) {
		o.cfg = eCfg.Config{
			ConfidenceParam: 13,
			EpochOffset:     3,
		}
		mc := newMockCacher()
		o.activesCache = mc

		// two target layers should produce different safe epochs
		// since the preliminary safe layers fall on either side of the epoch offset
		targetLayer1 := types.LayerID(44)
		targetLayer2 := types.LayerID(47)
		sls1, sle1 := safeLayerRange(
			targetLayer1,
			o.cfg.ConfidenceParam,
			o.layersPerEpoch,
			o.cfg.EpochOffset)
		r.Equal(20, int(sls1))
		r.Equal(23, int(sle1))
		sls2, sle2 := safeLayerRange(
			targetLayer2,
			o.cfg.ConfidenceParam,
			o.layersPerEpoch,
			o.cfg.EpochOffset)
		r.Equal(30, int(sls2))
		r.Equal(33, int(sle2))

		// make sure the result of the first is not read from the cache for the second even though both target layers
		// are in the same epoch
		v1, err := o.actives(context.TODO(), targetLayer1)
		r.NoError(err)
		v2, err := o.actives(context.TODO(), targetLayer2)
		r.NoError(err)
		r.Equal(v1, v2)
		r.Equal(2, mc.numAdd)
		r.Equal(2, mc.numGet) // two misses
	})
}

func TestOracle_concurrentActives(t *testing.T) {
	r := require.New(t)
	o := defaultOracle(t)
	o.atxdb = &mockActiveSetProvider{size: 1}

	mc := newMockCacher()
	o.activesCache = mc

	runFn := func() {
		_, err := o.actives(context.TODO(), 100)
		r.NoError(err)
	}

	// outstanding probability for concurrent access to calc active set size
	for i := 0; i < 100; i++ {
		go runFn()
	}

	// make sure we wait at least two calls duration
	runFn()
	runFn()

	r.Equal(1, mc.numAdd)
}

// make sure the oracle collects blocks from all layers in the safe layer range
func TestOracle_MultipleLayerBlocks(t *testing.T) {
	r := require.New(t)
	confidenceParam := uint16(25)
	epochOffset := uint16(5)

	// a block provider that returns one distinct block per layer
	lyr := types.LayerID(100)
	o := defaultOracle(t)
	o.cfg = eCfg.Config{ConfidenceParam: confidenceParam, EpochOffset: epochOffset}
	sls, sle := safeLayerRange(lyr, confidenceParam, defLayersPerEpoch, epochOffset)
	numLayers := int(sle - sls + 1) // +1 because inclusive of endpoints
	log.With().Info("layer range", log.FieldNamed("start", sls), log.FieldNamed("end", sle), log.Int("count", numLayers))
	allBlocks := make(map[types.BlockID]bool, numLayers)
	o.meshdb = &mockBlocksProvider{layerContextuallyValidBlocksFn: func(layerID types.LayerID) (map[types.BlockID]struct{}, error) {
		mp := make(map[types.BlockID]struct{}, 1)
		block := types.NewExistingBlock(layerID, layerID.Bytes(), nil)
		mp[block.ID()] = struct{}{}
		allBlocks[block.ID()] = false
		return mp, nil
	}}
	o.atxdb = &mockActiveSetProvider{size: 1, getActiveSetFn: func(epochID types.EpochID, mp map[types.BlockID]struct{}) (map[string]uint64, error) {
		// make sure we got the expected number of blocks
		r.Len(mp, numLayers, "expected one block per layer")
		// make sure each block was returned previously and that it's only been seen once
		for bid := range mp {
			r.Contains(allBlocks, bid, "unknown block")
			r.False(allBlocks[bid], "already saw this block")
			allBlocks[bid] = true
		}
		ret := make(map[string]uint64, 1)
		ret["foo"] = 1
		return ret, nil
	}}
	// make sure every block was seen
	for _, val := range allBlocks {
		r.True(val, "block was not seen")
	}
	_, err := o.actives(context.TODO(), lyr)
	r.Len(allBlocks, numLayers, "expected one block per layer")
	r.NoError(err)
}

func TestOracle_activesSafeLayer(t *testing.T) {
	r := require.New(t)
	confidenceParam := uint16(25)
	epochOffset := uint16(5)
	layersPerEpoch := uint16(2)

	// an activeSetProvider that returns an active set from blocks but no epoch ATXs: in this test we want to make sure
	// that hare active set succeeds and tortoise active set fails
	asp := &mockActiveSetProvider{size: 1, getEpochAtxsFn: func(types.EpochID) []types.ATXID { return nil }}

	o := mockOracle(t, layersPerEpoch)
	o.atxdb = asp
	o.cfg = eCfg.Config{ConfidenceParam: confidenceParam, EpochOffset: epochOffset}
	o.layersPerEpoch = layersPerEpoch
	o.activesCache = newMockCacher()
	lyr := types.LayerID(10)
	sls, sle := safeLayerRange(lyr, o.cfg.ConfidenceParam, o.layersPerEpoch, o.cfg.EpochOffset)
	mp := make(map[types.BlockID]struct{})
	block1 := types.NewExistingBlock(0, []byte("some data"), nil)
	mp[block1.ID()] = struct{}{}
	lastLayer := sls
	mbp := &mockBlocksProvider{layerContextuallyValidBlocksFn: func(layerID types.LayerID) (map[types.BlockID]struct{}, error) {
		// make sure it's called on each layer in the range, in turn
		r.GreaterOrEqual(uint64(layerID), uint64(sls), "LayerContextuallyValidBlocks called on layer before safeLayerStart")
		r.LessOrEqual(uint64(layerID), uint64(sle), "LayerContextuallyValidBlocks called on layer after safeLayerEnd")
		r.Equal(lastLayer, layerID, "LayerContextuallyValidBlocks called on layer out of order")
		lastLayer++
		return mp, nil
	}}
	o.meshdb = mbp
	res, err := o.actives(context.TODO(), lyr)
	// we don't care what the output is here, just that there is output and no error
	r.NotNil(res)
	r.NoError(err)

	bmp := make(map[types.LayerID]map[types.BlockID]struct{})
	mp2 := make(map[types.BlockID]struct{})
	mp2[block1.ID()] = struct{}{}
	bmp[lastLayer] = mp2
	o.meshdb = &mockBlocksProvider{mp: mp2}
	res, err = o.actives(context.TODO(), lyr)
	r.NotNil(res)
	r.NoError(err)
}

func TestOracle_HareToTortoiseFlow(t *testing.T) {
	r := require.New(t)
	types.SetLayersPerEpoch(10)

	// HARE ACTIVE SET SUCCEEDS: NO NEED TO CHECK TORTOISE ACTIVE SET

	// an activeSetProvider that returns an active set from blocks but no epoch ATXs: in this test we want to make sure
	// that hare active set succeeds and tortoise active set fails
	mp := make(map[types.BlockID]struct{})
	block1 := types.NewExistingBlock(0, []byte("some data"), nil)
	mp[block1.ID()] = struct{}{}
	asp := &mockActiveSetProvider{size: 1, getEpochAtxsFn: func(types.EpochID) []types.ATXID {
		r.Fail("tortoise active set should not be read")
		return nil
	}}
	var called1, called2, called3, called4 bool
	// blocksProvider that makes sure hare active set was read
	bp := &mockBlocksProvider{layerContextuallyValidBlocksFn: func(types.LayerID) (map[types.BlockID]struct{}, error) {
		called1 = true
		return mp, nil
	}}
	o := defaultOracle(t)
	o.atxdb = asp
	o.meshdb = bp
	o.activesCache = newMockCacher()
	lyr := types.LayerID(100)
	res, err := o.actives(context.TODO(), lyr)
	r.NotNil(res)
	r.NoError(err)
	r.True(called1, "hare active set wasn't read")

	// HARE ACTIVE SET EMPTY: CHECK TORTOISE ACTIVE SET

	// blocksProvider provides no blocks (so empty hare active set), activeSetProvider provides epoch ATXs
	asp = &mockActiveSetProvider{size: 1, getActiveSetFn: func(epochID types.EpochID, blockMap map[types.BlockID]struct{}) (map[string]uint64, error) {
		r.Zero(len(blockMap), "expected no hare blocks")
		return nil, nil
	}, getEpochAtxsFn: func(epochID types.EpochID) []types.ATXID {
		called2 = true
		return []types.ATXID{{}}
	}}
	mp2 := make(map[types.BlockID]struct{})
	o = defaultOracle(t)
	o.atxdb = asp
	o.meshdb = &mockBlocksProvider{mp: mp2}
	o.activesCache = newMockCacher()
	res, err = o.actives(context.TODO(), lyr)
	r.True(called2, "tortoise active set wasn't read")
	r.NotNil(res)
	r.NoError(err)

	// BOTH EMPTY: ERROR

	asp = &mockActiveSetProvider{size: 0, getActiveSetFn: func(epochID types.EpochID, blockMap map[types.BlockID]struct{}) (map[string]uint64, error) {
		r.Zero(len(blockMap), "expected no hare blocks")
		return nil, nil
	}, getEpochAtxsFn: func(epochID types.EpochID) []types.ATXID {
		called3 = true
		return nil
	}}
	// blocksProvider that makes sure hare active set was read
	bp = &mockBlocksProvider{layerContextuallyValidBlocksFn: func(types.LayerID) (map[types.BlockID]struct{}, error) {
		called4 = true
		// return no blocks
		return mp2, nil
	}}
	o = defaultOracle(t)
	o.atxdb = asp
	o.meshdb = bp
	o.activesCache = newMockCacher()
	res, err = o.actives(context.TODO(), lyr)
	r.Nil(res)
	r.Error(err)
	r.Contains(err.Error(), "empty active set for layer")
	r.True(called3, "tortoise active set wasn't read")
	r.True(called4, "hare active set wasn't read")
}

func TestOracle_IsIdentityActive(t *testing.T) {
	r := require.New(t)
	types.SetLayersPerEpoch(10)
	edid := "11111"
	atxdb := &mockActiveSetProvider{size: 1, getAtxHeaderFn: func(types.ATXID) (*types.ActivationTxHeader, error) {
		return &types.ActivationTxHeader{NIPSTChallenge: types.NIPSTChallenge{NodeID: types.NodeID{Key: edid}}}, nil
	}}
	o := mockOracle(t, uint16(5))
	o.atxdb = atxdb
	o.meshdb = &mockBlocksProvider{}
	v, err := o.IsIdentityActiveOnConsensusView(context.TODO(), "22222", 1)
	r.NoError(err)
	r.True(v)

	o.atxdb = &mockActiveSetProvider{size: 1, getActiveSetFn: func(types.EpochID, map[types.BlockID]struct{}) (map[string]uint64, error) {
		return nil, errFoo
	}}
	_, err = o.IsIdentityActiveOnConsensusView(context.TODO(), "22222", 100)
	r.Error(err)
	r.Equal(errFoo, errors.Unwrap(err))

	o.atxdb = &mockActiveSetProvider{size: 1, getActiveSetFn: func(types.EpochID, map[types.BlockID]struct{}) (map[string]uint64, error) {
		// return empty hare active set, forcing tortoise active set
		return nil, nil
	}, getAtxHeaderFn: func(types.ATXID) (*types.ActivationTxHeader, error) {
		return &types.ActivationTxHeader{NIPSTChallenge: types.NIPSTChallenge{NodeID: types.NodeID{Key: edid}}}, nil
	}}
	v, err = o.IsIdentityActiveOnConsensusView(context.TODO(), "22222", 100)
	r.NoError(err)
	r.False(v)

	v, err = o.IsIdentityActiveOnConsensusView(context.TODO(), edid, 100)
	r.NoError(err)
	r.True(v)
}

func TestOracle_CalcEligibility_withSpaceUnits(t *testing.T) {
	r := require.New(t)
	numOfMiners := 50
	committeeSize := 800
	types.SetLayersPerEpoch(10)
	o := defaultOracle(t)
	o.meshdb = &mockBlocksProvider{}
	o.atxdb = &mockActiveSetProvider{size: numOfMiners}

	var eligibilityCount uint16
	sig := make([]byte, 64)
	for pubkey := range createMapWithSize(numOfMiners) {
		n, err := rand.Read(sig)
		r.NoError(err)
		r.Equal(64, n)
		nodeID := types.NodeID{Key: pubkey}

		res, err := o.CalcEligibility(context.TODO(), types.LayerID(50), 1, committeeSize, nodeID, sig)
		r.NoError(err)

		valid, err := o.Validate(context.TODO(), types.LayerID(50), 1, committeeSize, nodeID, sig, res)
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
