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
var cfg = eCfg.Config{ConfidenceParam: 25, EpochOffset: 30}
var genWeight = uint64(5)

type mockBlocksProvider struct {
	mp map[types.BlockID]struct{}
}

func (mbp mockBlocksProvider) ContextuallyValidBlock(types.LayerID) (map[types.BlockID]struct{}, error) {
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
	size int
}

func (m *mockActiveSetProvider) ActiveSet(types.EpochID, map[types.BlockID]struct{}) (map[string]uint64, error) {
	return createMapWithSize(m.size), nil
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
	o := New(&mockValueProvider{1, nil}, (&mockActiveSetProvider{10}).ActiveSet, buildVerifier(true), &mockSigner{[]byte{1, 2, 3}}, 5, 1, 5, 1, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
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

func defaultOracle(t testing.TB) *Oracle {
	layersPerEpoch := uint16(defLayersPerEpoch)
	return mockOracle(t, layersPerEpoch)
}

func mockOracle(t testing.TB, layersPerEpoch uint16) *Oracle {
	types.SetLayersPerEpoch(int32(layersPerEpoch))
	o := New(&mockValueProvider{1, nil}, nil, buildVerifier(true), nil, layersPerEpoch, 1, genWeight, 1, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
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
	o.getActiveSet = func(epoch types.EpochID, view map[types.BlockID]struct{}) (map[string]uint64, error) {
		return nil, errFoo
	}

	res, err := o.CalcEligibility(context.TODO(), types.LayerID(50), 0, 1, types.NodeID{}, []byte{})

	r.EqualError(err, errFoo.Error())
	r.Equal(uint16(0), res)
}

func TestOracle_CalcEligibility_ZeroTotalWeight(t *testing.T) {
	r := require.New(t)
	o := defaultOracle(t)
	o.getActiveSet = (&mockActiveSetProvider{0}).ActiveSet

	res, err := o.CalcEligibility(context.TODO(), types.LayerID(cfg.ConfidenceParam*2+11), 0, 1, types.NodeID{}, []byte{})

	r.EqualError(err, "total weight is zero")
	r.Equal(uint16(0), res)
}

func TestOracle_CalcEligibility_ZeroCommitteeSize(t *testing.T) {
	r := require.New(t)
	o := defaultOracle(t)
	o.getActiveSet = (&mockActiveSetProvider{10}).ActiveSet

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
	o.getActiveSet = (&mockActiveSetProvider{numOfMiners}).ActiveSet

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
	o.getActiveSet = (&mockActiveSetProvider{numOfMiners}).ActiveSet

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

func Test_safeLayer(t *testing.T) {
	const safety = 25
	assert.Equal(t, types.GetEffectiveGenesis(), safeLayer(1, safety))
	assert.Equal(t, types.LayerID(100-safety), safeLayer(100, safety))
}

type mockBufferedActiveSetProvider struct {
	size map[types.EpochID]int
}

func (m *mockBufferedActiveSetProvider) ActiveSet(epoch types.EpochID, _ map[types.BlockID]struct{}) (map[string]uint64, error) {
	v, ok := m.size[epoch]
	if !ok {
		return createMapWithSize(0), fmt.Errorf("no size for epoch %v", epoch)
	}

	return createMapWithSize(v), nil
}

func createMapWithSize(n int) map[string]uint64 {
	m := make(map[string]uint64)
	for i := 0; i < n; i++ {
		m[strconv.Itoa(i)] = uint64(i + 1)
	}

	return m
}

func Test_ActiveSetSize(t *testing.T) {
	m := make(map[types.EpochID]int)
	m[types.EpochID(3)] = 2
	m[types.EpochID(4)] = 3
	m[types.EpochID(5)] = 5
	o := defaultOracle(t)
	o.getActiveSet = (&mockBufferedActiveSetProvider{m}).ActiveSet
	l := 19 + defSafety
	assertActiveSetSize(t, o, epochWeight(2), l)
	assertActiveSetSize(t, o, epochWeight(3), l+10)
	assertActiveSetSize(t, o, epochWeight(5), l+20)

	// create error
	o.activesCache, _ = lru.New(activesCacheSize)
	o.getActiveSet = func(epoch types.EpochID, view map[types.BlockID]struct{}) (map[string]uint64, error) {
		return createMapWithSize(5), errors.New("fake err")
	}
	activeSetSize, err := o.totalWeight(l + 19)
	assert.EqualError(t, err, "fake err")
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
	activeSetSize, err := o.totalWeight(l)
	assert.NoError(t, err)
	assert.Equal(t, expected, activeSetSize)
}

func Test_VrfSignVerify(t *testing.T) {
	o := defaultOracle(t)
	getActiveSet := func(types.EpochID, map[types.BlockID]struct{}) (map[string]uint64, error) {
		return map[string]uint64{
			"my_key": 1 * 1024,
			"abc":    9 * 1024,
		}, nil
	}
	o.getActiveSet = getActiveSet
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
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, 5, 1, genWeight, 1, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	o.getActiveSet = func(epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]uint64, error) {
		return createMapWithSize(17), nil
	}
	v1, e := o.totalWeight(defSafety + 100)
	r.NoError(e)

	o.getActiveSet = func(epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]uint64, error) {
		return createMapWithSize(19), nil
	}
	v2, e := o.totalWeight(defSafety + 100)
	r.NoError(e)
	r.Equal(v1, v2)
}

func TestOracle_roundedSafeLayer(t *testing.T) {
	const offset = 3
	r := require.New(t)
	types.SetLayersPerEpoch(1)
	v := roundedSafeLayer(1, 1, 1, offset)
	r.Equal(types.GetEffectiveGenesis(), v)
	types.SetLayersPerEpoch(1)
	v = roundedSafeLayer(1, 5, 1, offset)
	r.Equal(types.GetEffectiveGenesis(), v)
	types.SetLayersPerEpoch(10)
	v = roundedSafeLayer(50, 5, 10, offset)
	r.Equal(types.LayerID(43), v)
	types.SetLayersPerEpoch(4)
	v = roundedSafeLayer(2, 1, 4, 2)
	r.Equal(types.GetEffectiveGenesis(), v)
	v = roundedSafeLayer(10, 1, 4, offset)
	r.Equal(types.LayerID(4+offset), v)

	// examples
	// sl is after rounded layer
	types.SetLayersPerEpoch(5)
	v = roundedSafeLayer(types.GetEffectiveGenesis()+11, 5, 5, 1)
	r.Equal(types.LayerID(11), v)
	// sl is before rounded layer
	types.SetLayersPerEpoch(5)
	v = roundedSafeLayer(11, 5, 5, 3)
	r.Equal(types.LayerID(9), v)
}

func TestOracle_actives(t *testing.T) {
	r := require.New(t)
	o := defaultOracle(t)
	_, err := o.actives(1)
	r.EqualError(err, errGenesis.Error())

	o.blocksProvider = mockBlocksProvider{mp: make(map[types.BlockID]struct{})}
	_, err = o.actives(100)
	r.EqualError(err, errNoContextualBlocks.Error())

	o.blocksProvider = mockBlocksProvider{}
	mp := createMapWithSize(9)
	o.getActiveSet = func(epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]uint64, error) {
		return mp, nil
	}
	o.activesCache = newMockCacher()
	v, err := o.actives(100)
	r.NoError(err)
	v2, err := o.actives(100)
	r.NoError(err)
	r.Equal(v, v2)
	for k := range mp {
		_, exist := v[k]
		r.True(exist)
	}

	o.getActiveSet = func(epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]uint64, error) {
		return createMapWithSize(9), errFoo
	}
	_, err = o.actives(200)
	r.Equal(errFoo, err)
}

func TestOracle_concurrentActives(t *testing.T) {
	r := require.New(t)
	o := defaultOracle(t)

	mc := newMockCacher()
	o.activesCache = mc
	mp := createMapWithSize(9)
	o.getActiveSet = func(epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]uint64, error) {
		return mp, nil
	}

	// outstanding probability for concurrent access to calc active set size
	for i := 0; i < 100; i++ {
		//goland:noinspection ALL
		go o.actives(100)
	}

	// make sure we wait at least two calls duration
	_, _ = o.actives(100)
	_, _ = o.actives(100)

	r.Equal(1, mc.numAdd)
}

type bProvider struct {
	mp map[types.LayerID]map[types.BlockID]struct{}
}

func (p *bProvider) ContextuallyValidBlock(layer types.LayerID) (map[types.BlockID]struct{}, error) {
	if mp, exist := p.mp[layer]; exist {
		return mp, nil
	}

	return nil, errors.New("does not exist")
}

func TestOracle_activesSafeLayer(t *testing.T) {
	r := require.New(t)
	o := mockOracle(t, 2)
	o.layersPerEpoch = 2
	o.cfg = eCfg.Config{ConfidenceParam: 2, EpochOffset: 0}
	mp := createMapWithSize(9)
	o.activesCache = newMockCacher()
	lyr := 19 + defSafety
	rsl := roundedSafeLayer(lyr, types.LayerID(o.cfg.ConfidenceParam), o.layersPerEpoch, types.LayerID(o.cfg.EpochOffset))
	o.getActiveSet = func(epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]uint64, error) {
		ep := rsl.GetEpoch()
		r.Equal(ep-1, epoch)
		return mp, nil
	}

	bmp := make(map[types.LayerID]map[types.BlockID]struct{})
	mp2 := make(map[types.BlockID]struct{})
	block1 := types.NewExistingBlock(0, []byte("some data"), nil)
	mp2[block1.ID()] = struct{}{}
	bmp[rsl] = mp2
	o.blocksProvider = &bProvider{bmp}
	mpRes, err := o.actives(lyr)
	r.NoError(err)
	r.NotNil(mpRes)
}

func TestOracle_IsIdentityActive(t *testing.T) {
	r := require.New(t)
	o := defaultOracle(t)
	mp := make(map[string]uint64)
	edid := "11111"
	mp[edid] = 11
	o.getActiveSet = func(epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]uint64, error) {
		return mp, nil
	}
	v, err := o.IsIdentityActiveOnConsensusView("22222", 1)
	r.NoError(err)
	r.True(v)

	o.getActiveSet = func(epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]uint64, error) {
		return mp, errFoo
	}
	_, err = o.IsIdentityActiveOnConsensusView("22222", 100)
	r.Equal(errFoo, err)

	o.getActiveSet = func(epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]uint64, error) {
		return mp, nil
	}

	v, err = o.IsIdentityActiveOnConsensusView("22222", 100)
	r.NoError(err)
	r.False(v)

	v, err = o.IsIdentityActiveOnConsensusView(edid, 100)
	r.NoError(err)
	r.True(v)
}

func TestOracle_CalcEligibility_withSpaceUnits(t *testing.T) {
	r := require.New(t)
	numOfMiners := 50
	committeeSize := 800
	types.SetLayersPerEpoch(10)
	o := New(&mockValueProvider{1, nil}, nil, buildVerifier(true), nil, 10, 1, genWeight, 1, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	o.getActiveSet = (&mockActiveSetProvider{numOfMiners}).ActiveSet

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
