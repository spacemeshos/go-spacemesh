package eligibility

import (
	"context"
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
	eCfg "github.com/spacemeshos/go-spacemesh/hare/eligibility/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const defSafety = types.LayerID(25)
const defLayersPerEpoch = 10

var errFoo = errors.New("some error")
var errMy = errors.New("my error")
var cfg = eCfg.Config{ConfidenceParam: 25, EpochOffset: 30}
var genActive = 5

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

func (mvp *mockValueProvider) Value(types.LayerID) (uint32, error) {
	return mvp.val, mvp.err
}

type mockActiveSetProvider struct {
	size int
}

func (m *mockActiveSetProvider) ActiveSet(context.Context, types.EpochID, map[types.BlockID]struct{}) (map[string]struct{}, error) {
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

func (s *mockSigner) Sign(msg []byte) []byte {
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
	o := New(&mockValueProvider{1, nil}, (&mockActiveSetProvider{10}).ActiveSet, buildVerifier(true), &mockSigner{[]byte{1, 2, 3}}, 5, 5, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
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
	types.SetLayersPerEpoch(defLayersPerEpoch)
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, 0, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	o.layersPerEpoch = 10
	o.vrfVerifier = buildVerifier(false)
	res, err := o.Eligible(context.TODO(), types.LayerID(1), 0, 1, types.NodeID{}, []byte{})
	assert.NoError(t, err)
	assert.False(t, res)

	o.vrfVerifier = buildVerifier(true)
	o.getActiveSet = (&mockActiveSetProvider{10}).ActiveSet
	res, err = o.Eligible(context.TODO(), types.LayerID(50), 1, 0, types.NodeID{}, []byte{})
	assert.Nil(t, err)
	assert.False(t, res)

	o.getActiveSet = (&mockActiveSetProvider{0}).ActiveSet
	res, err = o.Eligible(context.TODO(), types.LayerID(cfg.ConfidenceParam*2+11), 1, 0, types.NodeID{}, []byte{})
	assert.NotNil(t, err)
	assert.Equal(t, "active set size is zero", err.Error())
	assert.False(t, res)

	o.getActiveSet = (&mockActiveSetProvider{10}).ActiveSet
	res, err = o.Eligible(context.TODO(), types.LayerID(50), 1, 10, types.NodeID{}, []byte{})
	assert.Nil(t, err)
	assert.True(t, res)
}

func Test_safeLayer(t *testing.T) {
	const safety = 25
	assert.Equal(t, types.GetEffectiveGenesis(), safeLayer(1, safety))
	assert.Equal(t, types.LayerID(100-safety), safeLayer(100, safety))
}

func Test_ZeroParticipants(t *testing.T) {
	o := New(&mockValueProvider{1, nil}, (&mockActiveSetProvider{5}).ActiveSet, buildVerifier(true), &mockSigner{}, defLayersPerEpoch, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	res, err := o.Eligible(context.TODO(), 1, 0, 0, types.NodeID{Key: ""}, []byte{1})
	assert.Nil(t, err)
	assert.False(t, res)
}

func Test_AllParticipants(t *testing.T) {
	o := New(&mockValueProvider{1, nil}, (&mockActiveSetProvider{5}).ActiveSet, buildVerifier(true), &mockSigner{}, 10, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	res, err := o.Eligible(context.TODO(), 0, 0, 5, types.NodeID{Key: ""}, []byte{1})
	assert.Nil(t, err)
	assert.True(t, res)
}

func genBytes() []byte {
	rnd := make([]byte, 1000)
	rand.Seed(time.Now().UnixNano())
	rand.Read(rnd)

	return rnd
}

func Test_ExpectedCommitteeSize(t *testing.T) {
	setSize := 1024
	commSize := 1000
	o := New(&mockValueProvider{1, nil}, (&mockActiveSetProvider{setSize}).ActiveSet, buildVerifier(true), &mockSigner{}, 10, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	count := 0
	for i := 0; i < setSize; i++ {
		res, err := o.Eligible(context.TODO(), 0, 0, commSize, types.NodeID{Key: ""}, genBytes())
		assert.Nil(t, err)
		if res {
			count++
		}
	}

	dev := 10 * commSize / 100
	cond := count > commSize-dev && count < commSize+dev
	assert.True(t, cond)
}

type mockBufferedActiveSetProvider struct {
	size map[types.EpochID]int
}

func (m *mockBufferedActiveSetProvider) ActiveSet(ctx context.Context, epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
	v, ok := m.size[epoch]
	if !ok {
		return createMapWithSize(0), errors.New("no instance")
	}

	return createMapWithSize(v), nil
}

func createMapWithSize(n int) map[string]struct{} {
	m := make(map[string]struct{})
	for i := 0; i < n; i++ {
		m[strconv.Itoa(i)] = struct{}{}
	}

	return m
}

func Test_ActiveSetSize(t *testing.T) {
	t.Skip()
	m := make(map[types.EpochID]int)
	m[types.EpochID(19)] = 2
	m[types.EpochID(29)] = 3
	m[types.EpochID(39)] = 5
	o := New(&mockValueProvider{1, nil}, (&mockBufferedActiveSetProvider{m}).ActiveSet, buildVerifier(true), &mockSigner{}, 10, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	// TODO: remove this comment after inception problem is addressed
	// assert.Equal(t, o.getActiveSet.ActiveSet(0), o.activeSetSize(1))
	l := 19 + defSafety
	assertActiveSetSize(t, o, 2, l)
	assertActiveSetSize(t, o, 3, l+10)
	assertActiveSetSize(t, o, 5, l+20)

	// create error
	o.getActiveSet = func(ctx context.Context, epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return createMapWithSize(5), errors.New("fake err")
	}
	activeSetSize, err := o.activeSetSize(context.TODO(), l+19)
	assert.Error(t, err)
	assert.Equal(t, uint32(0), activeSetSize)
}

func assertActiveSetSize(t *testing.T, o *Oracle, expected uint32, l types.LayerID) {
	activeSetSize, err := o.activeSetSize(context.TODO(), l)
	assert.NoError(t, err)
	assert.Equal(t, expected, activeSetSize)
}

func Test_VrfSignVerify(t *testing.T) {
	seed := make([]byte, 32)
	rand.Read(seed)
	vrfSigner, vrfPubkey, err := signing.NewVRFSigner(seed)
	assert.NoError(t, err)
	o := New(&mockValueProvider{1, nil}, (&mockActiveSetProvider{10}).ActiveSet, signing.VRFVerify, vrfSigner, 10, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	getActiveSet := func(context.Context, types.EpochID, map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return map[string]struct{}{
			"my_key": {},
			"abc":    {},
		}, nil
	}
	o.getActiveSet = getActiveSet
	id := types.NodeID{Key: "my_key", VRFPublicKey: vrfPubkey}
	proof, err := o.Proof(context.TODO(), 50, 1)
	assert.NoError(t, err)

	res, err := o.Eligible(context.TODO(), 50, 1, 10, id, proof)
	assert.NoError(t, err)
	assert.True(t, res)
}

func TestOracle_Proof(t *testing.T) {
	o := New(&mockValueProvider{0, errMy}, (&mockActiveSetProvider{10}).ActiveSet, buildVerifier(true), &mockSigner{}, 10, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	sig, err := o.Proof(context.TODO(), 2, 3)
	assert.Nil(t, sig)
	assert.NotNil(t, err)
	assert.Equal(t, errMy, err)
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

func TestOracle_Eligible(t *testing.T) {
	o := New(&mockValueProvider{0, errMy}, (&mockActiveSetProvider{10}).ActiveSet, buildVerifier(true), &mockSigner{}, 10, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	res, err := o.Eligible(context.TODO(), 1, 2, 3, types.NodeID{}, []byte{})
	assert.False(t, res)
	assert.NotNil(t, err)
	assert.Equal(t, errMy, err)

	o.beacon = &mockValueProvider{0, nil}
	o.vrfVerifier = buildVerifier(false)
	res, err = o.Eligible(context.TODO(), 1, 2, 3, types.NodeID{}, []byte{})
	assert.False(t, res)
	assert.Nil(t, err)
}

func TestOracle_activeSetSizeCache(t *testing.T) {
	r := require.New(t)
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, 5, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	o.getActiveSet = func(ctx context.Context, epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return createMapWithSize(17), nil
	}
	v1, e := o.activeSetSize(context.TODO(), defSafety+100)
	r.NoError(e)

	o.getActiveSet = func(ctx context.Context, epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return createMapWithSize(19), nil
	}
	v2, e := o.activeSetSize(context.TODO(), defSafety+100)
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
	types.SetLayersPerEpoch(10)
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, 5, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	_, err := o.actives(context.TODO(), 1)
	r.EqualError(err, errGenesis.Error())

	o.blocksProvider = mockBlocksProvider{mp: make(map[types.BlockID]struct{})}
	_, err = o.actives(context.TODO(), 100)
	r.NoError(err)

	o.blocksProvider = mockBlocksProvider{}
	mp := createMapWithSize(9)
	o.getActiveSet = func(ctx context.Context, epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return mp, nil
	}
	o.activesCache = newMockCacher()
	v, err := o.actives(context.TODO(), 100)
	r.NoError(err)
	v2, err := o.actives(context.TODO(), 100)
	r.NoError(err)
	r.Equal(v, v2)
	for k := range mp {
		_, exist := v[k]
		r.True(exist)
	}

	o.getActiveSet = func(ctx context.Context, epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return createMapWithSize(9), errFoo
	}
	_, err = o.actives(context.TODO(), 200)
	r.Equal(errFoo, err)
}

func TestOracle_concurrentActives(t *testing.T) {
	r := require.New(t)
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, 5, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))

	mc := newMockCacher()
	o.activesCache = mc
	mp := createMapWithSize(9)
	o.getActiveSet = func(ctx context.Context, epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return mp, nil
	}

	// outstanding probability for concurrent access to calc active set size
	for i := 0; i < 100; i++ {
		go o.actives(context.TODO(), 100)
	}

	// make sure we wait at least two calls duration
	o.actives(context.TODO(), 100)
	o.actives(context.TODO(), 100)

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
	types.SetLayersPerEpoch(2)
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, 2, genActive, mockBlocksProvider{}, eCfg.Config{ConfidenceParam: 2, EpochOffset: 0}, log.NewDefault(t.Name()))
	mp := createMapWithSize(9)
	o.activesCache = newMockCacher()
	lyr := types.LayerID(10)
	rsl := roundedSafeLayer(lyr, types.LayerID(o.cfg.ConfidenceParam), o.layersPerEpoch, types.LayerID(o.cfg.EpochOffset))
	o.getActiveSet = func(ctx context.Context, epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
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
	mpRes, err := o.actives(context.TODO(), lyr)
	r.NotNil(mpRes)
	r.NoError(err)
}

func TestOracle_activesNoContextuallyValid(t *testing.T) {
	r := require.New(t)
	layersPerEpoch := 20
	types.SetLayersPerEpoch(int32(layersPerEpoch))
	mp := createMapWithSize(9)
	lyr := types.LayerID(100)

	// This should fail since hDist is too low to allow checking any other layers
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, 2, genActive, mockBlocksProvider{}, eCfg.Config{ConfidenceParam: 10, EpochOffset: 0}, log.NewDefault(t.Name()))
	o.layersPerEpoch = uint16(layersPerEpoch)
	o.activesCache = newMockCacher()
	rsl := roundedSafeLayer(lyr, types.LayerID(o.cfg.ConfidenceParam), o.layersPerEpoch, types.LayerID(o.cfg.EpochOffset))
	r.Equal(types.LayerID(80), rsl)
	o.getActiveSet = func(ctx context.Context, epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		ep := rsl.GetEpoch()
		r.Equal(ep-1, epoch)
		return mp, nil
	}

	bmp := make(map[types.LayerID]map[types.BlockID]struct{})
	mp1 := make(map[types.BlockID]struct{})
	bmp[rsl] = mp1
	o.blocksProvider = &bProvider{bmp}
	mpRes, err := o.actives(context.TODO(), lyr)

	// This should not fail but it should return an empty set
	r.NoError(err)
	r.Len(mpRes, 0)
}

func TestOracle_IsIdentityActive(t *testing.T) {
	r := require.New(t)
	types.SetLayersPerEpoch(10)
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, 5, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	mp := make(map[string]struct{})
	edid := "11111"
	mp[edid] = struct{}{}
	o.getActiveSet = func(ctx context.Context, epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return mp, nil
	}
	v, err := o.IsIdentityActiveOnConsensusView(context.TODO(), "22222", 1)
	r.NoError(err)
	r.True(v)

	o.getActiveSet = func(ctx context.Context, epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return mp, errFoo
	}
	_, err = o.IsIdentityActiveOnConsensusView(context.TODO(), "22222", 100)
	r.Equal(errFoo, err)

	o.getActiveSet = func(ctx context.Context, epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return mp, nil
	}

	v, err = o.IsIdentityActiveOnConsensusView(context.TODO(), "22222", 100)
	r.NoError(err)
	r.False(v)

	v, err = o.IsIdentityActiveOnConsensusView(context.TODO(), edid, 100)
	r.NoError(err)
	r.True(v)
}

func TestOracle_Eligible2(t *testing.T) {
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, 5, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	types.SetLayersPerEpoch(10)
	o.getActiveSet = func(ctx context.Context, epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return createMapWithSize(9), errFoo
	}
	o.vrfVerifier = func(pub, msg, sig []byte) bool {
		return true
	}
	_, err := o.Eligible(context.TODO(), 100, 1, 1, types.NodeID{}, []byte{})
	assert.Equal(t, errFoo, err)
}

func TestOracleActiveSetCacheWarming(t *testing.T) {
	r := require.New(t)
	types.SetLayersPerEpoch(defLayersPerEpoch)
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, defLayersPerEpoch, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	mc := newMockCacher()
	o.activesCache = mc
	mapSize := 9
	mp := createMapWithSize(mapSize)
	callCount := int32(0)
	o.getActiveSet = func(ctx context.Context, epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		// simulate slow IO
		time.Sleep(time.Second)
		atomic.AddInt32(&callCount, 1)
		return mp, nil
	}

	// set firstLayer to two layers before a safe epoch boundary
	r.Equal(2, cacheWarmingDistance)
	firstLayer := types.LayerID(72)
	sl1 := roundedSafeLayer(firstLayer, types.LayerID(o.cfg.ConfidenceParam), o.layersPerEpoch, types.LayerID(o.cfg.EpochOffset))
	r.Equal(60, int(sl1))
	sl2 := roundedSafeLayer(firstLayer.Add(1), types.LayerID(o.cfg.ConfidenceParam), o.layersPerEpoch, types.LayerID(o.cfg.EpochOffset))
	r.Equal(60, int(sl2))
	sl3 := roundedSafeLayer(firstLayer.Add(2), types.LayerID(o.cfg.ConfidenceParam), o.layersPerEpoch, types.LayerID(o.cfg.EpochOffset))
	r.Equal(60, int(sl3))
	sl4 := roundedSafeLayer(firstLayer.Add(3), types.LayerID(o.cfg.ConfidenceParam), o.layersPerEpoch, types.LayerID(o.cfg.EpochOffset))
	r.Equal(70, int(sl4))
	r.Equal(int(sl1.GetEpoch()), int(sl2.GetEpoch()))
	r.Equal(int(sl2.GetEpoch()), int(sl3.GetEpoch()))
	r.NotEqual(int(sl3.GetEpoch()), int(sl4.GetEpoch()))

	// 3 layers before transition to new safe epoch: no cache warming
	activeMap, err := o.actives(context.TODO(), firstLayer)
	r.NoError(err)
	r.Len(activeMap, mapSize)
	r.Equal(1, int(atomic.LoadInt32(&callCount)))
	r.Equal(1, mc.numAdd)
	r.Equal(1, mc.numGet)
	r.Contains(mc.data, sl1.GetEpoch())
	r.NotContains(mc.data, sl4.GetEpoch())

	// 2 layers before transition to new safe epoch: should warm cache concurrently
	activeMap, err = o.actives(context.TODO(), firstLayer.Add(1))
	r.NoError(err)
	r.Len(activeMap, mapSize)
	r.Equal(1, mc.numAdd)
	r.Equal(2, mc.numGet)
	r.Contains(mc.data, sl1.GetEpoch())
	r.NotContains(mc.data, sl4.GetEpoch())

	// this happens concurrently
	r.Eventually(func() bool {
		_, contains := mc.data[sl4.GetEpoch()]
		return contains && atomic.LoadInt32(&callCount) == int32(2) && mc.numAdd == 2
	}, 2*time.Second, time.Millisecond*100)

	// 1 layer before transition to new safe epoch: warming should've happened already
	done1 := make(chan error)
	go func() {
		// should not block on active set generation
		_, err = o.actives(context.TODO(), firstLayer.Add(2))
		done1 <- err
	}()
	select {
	case err := <-done1:
		t.Log("active set generation did not block")
		r.NoError(err)
	case <-time.After(500 * time.Millisecond):
		r.Fail("timed out waiting for active set generation")
	}
	r.NoError(err)
	r.Equal(2, mc.numAdd)
	r.Equal(3, mc.numGet)
	r.Contains(mc.data, sl1.GetEpoch())
	r.Contains(mc.data, sl4.GetEpoch())

	// get active set first layer of new safe epoch: should be no delay
	done2 := make(chan error)
	go func() {
		// should not block on active set generation
		_, err = o.actives(context.TODO(), firstLayer.Add(3))
		done2 <- err
	}()
	select {
	case err := <-done2:
		t.Log("active set generation did not block")
		r.NoError(err)
	case <-time.After(500 * time.Millisecond):
		r.Fail("timed out waiting for active set generation")
	}
	r.Equal(2, int(atomic.LoadInt32(&callCount)))
	r.Equal(2, mc.numAdd)
	r.Equal(4, mc.numGet)
	r.Contains(mc.data, sl1.GetEpoch())
	r.Contains(mc.data, sl4.GetEpoch())
}
