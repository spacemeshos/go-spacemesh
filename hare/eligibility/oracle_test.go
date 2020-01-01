package eligibility

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/amcl/BLS381"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	eCfg "github.com/spacemeshos/go-spacemesh/hare/eligibility/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math/rand"
	"strconv"
	"sync"
	"testing"
	"time"
)

const defSafety = types.LayerID(25)
const defLayersPerEpoch = 10
const defNonGenesisLayer = defLayersPerEpoch*2 + 1

var someErr = errors.New("some error")
var myErr = errors.New("my error")
var cfg = eCfg.Config{25, 30}
var genActive = 5

type mockBlocksProvider struct {
	mp map[types.BlockID]struct{}
}

func (mbp mockBlocksProvider) ContextuallyValidBlock(layer types.LayerID) (map[types.BlockID]struct{}, error) {
	if mbp.mp == nil {
		mbp.mp = make(map[types.BlockID]struct{})
		block1 := types.NewExistingBlock(0, []byte("some data"))
		mbp.mp[block1.Id()] = struct{}{}
	}
	return mbp.mp, nil
}

type mockValueProvider struct {
	val uint32
	err error
}

func (mvp *mockValueProvider) Value(layer types.LayerID) (uint32, error) {
	return mvp.val, mvp.err
}

type mockActiveSetProvider struct {
	size int
}

func (m *mockActiveSetProvider) ActiveSet(epoch types.EpochId, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
	return createMapWithSize(m.size), nil
}

func buildVerifier(result bool, err error) verifierFunc {
	return func(msg, sig []byte, pub []byte) (bool, error) {
		return result, err
	}
}

type mockSigner struct {
	sig []byte
	err error
}

func (s *mockSigner) Sign(msg []byte) ([]byte, error) {
	return s.sig, s.err
}

type mockCacher struct {
	data   map[interface{}]interface{}
	numAdd int
	numGet int
	mutex  sync.Mutex
}

func newMockCasher() *mockCacher {
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
	o := Oracle{vrfMsgCache: newMockCasher(), Log: log.NewDefault(t.Name())}
	o.beacon = &mockValueProvider{1, someErr}
	_, err := o.buildVRFMessage(types.LayerID(1), 1)
	r.Equal(someErr, err)

	o.beacon = &mockValueProvider{1, nil}
	m, err := o.buildVRFMessage(1, 2)
	r.NoError(err)
	m2, ok := o.vrfMsgCache.Get(buildKey(1, 2))
	r.True(ok)
	r.Equal(m, m2) // check same as in cache

	// check not same for different round
	m4, err := o.buildVRFMessage(1, 3)
	r.NoError(err)
	r.NotEqual(m, m4)

	// check not same for different layer
	m5, err := o.buildVRFMessage(2, 2)
	r.NoError(err)
	r.NotEqual(m, m5)

	o.beacon = &mockValueProvider{5, nil} // set different value
	m3, err := o.buildVRFMessage(1, 2)
	r.NoError(err)
	r.Equal(m, m3) // check same result (from cache)
}

func TestOracle_buildVRFMessageConcurrency(t *testing.T) {
	r := require.New(t)
	o := New(&mockValueProvider{1, nil}, (&mockActiveSetProvider{10}).ActiveSet, buildVerifier(true, nil), &mockSigner{[]byte{1, 2, 3}, nil}, 5, 5, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	mCache := newMockCasher()
	o.vrfMsgCache = mCache

	total := 1000
	wg := sync.WaitGroup{}
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			_, err := o.buildVRFMessage(1, int32(i%10))
			r.NoError(err)
			wg.Done()
		}()
	}

	wg.Wait()
	r.Equal(10, mCache.numAdd)
}

func TestOracle_IsEligible(t *testing.T) {
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, 0, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	o.layersPerEpoch = 10
	o.vrfVerifier = buildVerifier(false, someErr)
	res, err := o.Eligible(types.LayerID(1), 0, 1, types.NodeId{}, []byte{})
	assert.NotNil(t, err)
	assert.False(t, res)

	o.vrfVerifier = buildVerifier(true, nil)
	o.getActiveSet = (&mockActiveSetProvider{10}).ActiveSet
	res, err = o.Eligible(types.LayerID(50), 1, 0, types.NodeId{}, []byte{})
	assert.Nil(t, err)
	assert.False(t, res)

	o.getActiveSet = (&mockActiveSetProvider{0}).ActiveSet
	res, err = o.Eligible(types.LayerID(cfg.ConfidenceParam+11), 1, 0, types.NodeId{}, []byte{})
	assert.NotNil(t, err)
	assert.Equal(t, "active set size is zero", err.Error())
	assert.False(t, res)

	o.getActiveSet = (&mockActiveSetProvider{10}).ActiveSet
	res, err = o.Eligible(types.LayerID(50), 1, 10, types.NodeId{}, []byte{})
	assert.Nil(t, err)
	assert.True(t, res)
}

func Test_safeLayer(t *testing.T) {
	const safety = 25
	assert.Equal(t, config.Genesis, safeLayer(1, safety))
	assert.Equal(t, types.LayerID(100-safety), safeLayer(100, safety))
}

func Test_ZeroParticipants(t *testing.T) {
	o := New(&mockValueProvider{1, nil}, (&mockActiveSetProvider{5}).ActiveSet, buildVerifier(true, nil), &mockSigner{}, defLayersPerEpoch, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	res, err := o.Eligible(1, 0, 0, types.NodeId{Key: ""}, []byte{1})
	assert.Nil(t, err)
	assert.False(t, res)
}

func Test_AllParticipants(t *testing.T) {
	o := New(&mockValueProvider{1, nil}, (&mockActiveSetProvider{5}).ActiveSet, buildVerifier(true, nil), &mockSigner{}, 10, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	res, err := o.Eligible(0, 0, 5, types.NodeId{Key: ""}, []byte{1})
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
	o := New(&mockValueProvider{1, nil}, (&mockActiveSetProvider{setSize}).ActiveSet, buildVerifier(true, nil), &mockSigner{}, 10, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	count := 0
	for i := 0; i < setSize; i++ {
		res, err := o.Eligible(0, 0, commSize, types.NodeId{Key: ""}, genBytes())
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
	size map[types.EpochId]int
}

func (m *mockBufferedActiveSetProvider) ActiveSet(epoch types.EpochId, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
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
	m := make(map[types.EpochId]int)
	m[types.EpochId(19)] = 2
	m[types.EpochId(29)] = 3
	m[types.EpochId(39)] = 5
	o := New(&mockValueProvider{1, nil}, (&mockBufferedActiveSetProvider{m}).ActiveSet, buildVerifier(true, nil), &mockSigner{}, 10, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	// TODO: remove this comment after inception problem is addressed
	//assert.Equal(t, o.getActiveSet.ActiveSet(0), o.activeSetSize(1))
	l := 19 + defSafety
	assertActiveSetSize(t, o, 2, l)
	assertActiveSetSize(t, o, 3, l+10)
	assertActiveSetSize(t, o, 5, l+20)

	// create error
	o.getActiveSet = func(epoch types.EpochId, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {

		return createMapWithSize(5), errors.New("fake err")
	}
	activeSetSize, err := o.activeSetSize(l + 19)
	assert.Error(t, err)
	assert.Equal(t, uint32(0), activeSetSize)
}

func assertActiveSetSize(t *testing.T, o *Oracle, expected uint32, l types.LayerID) {
	activeSetSize, err := o.activeSetSize(l)
	assert.NoError(t, err)
	assert.Equal(t, expected, activeSetSize)
}

func Test_BlsSignVerify(t *testing.T) {
	pr, pu := BLS381.GenKeyPair(BLS381.DefaultSeed())
	sr := BLS381.NewBlsSigner(pr)
	o := New(&mockValueProvider{1, nil}, (&mockActiveSetProvider{10}).ActiveSet, BLS381.Verify2, sr, 10, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	id := types.NodeId{Key: "abc", VRFPublicKey: pu}
	proof, err := o.Proof(1, 1)
	assert.Nil(t, err)
	res, err := o.Eligible(1, 1, 10, id, proof)
	assert.Nil(t, err)
	assert.True(t, res)
}

func TestOracle_Proof(t *testing.T) {
	o := New(&mockValueProvider{0, myErr}, (&mockActiveSetProvider{10}).ActiveSet, buildVerifier(true, nil), &mockSigner{}, 10, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	sig, err := o.Proof(2, 3)
	assert.Nil(t, sig)
	assert.NotNil(t, err)
	assert.Equal(t, myErr, err)
	o.beacon = &mockValueProvider{0, nil}
	o.vrfSigner = &mockSigner{nil, myErr}
	sig, err = o.Proof(2, 3)
	assert.Nil(t, sig)
	assert.NotNil(t, err)
	assert.Equal(t, myErr, err)
	mySig := []byte{1, 2}
	o.vrfSigner = &mockSigner{mySig, nil}
	sig, err = o.Proof(2, 3)
	assert.Nil(t, err)
	assert.Equal(t, mySig, sig)
}

func TestOracle_Eligible(t *testing.T) {
	o := New(&mockValueProvider{0, myErr}, (&mockActiveSetProvider{10}).ActiveSet, buildVerifier(true, nil), &mockSigner{}, 10, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	res, err := o.Eligible(1, 2, 3, types.NodeId{}, []byte{})
	assert.False(t, res)
	assert.NotNil(t, err)
	assert.Equal(t, myErr, err)

	o.beacon = &mockValueProvider{0, nil}
	o.vrfVerifier = buildVerifier(false, nil)
	res, err = o.Eligible(1, 2, 3, types.NodeId{}, []byte{})
	assert.False(t, res)
	assert.Nil(t, err)
}

func TestOracle_activeSetSizeCache(t *testing.T) {
	r := require.New(t)
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, 5, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	o.getActiveSet = func(epoch types.EpochId, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return createMapWithSize(17), nil
	}
	v1, e := o.activeSetSize(defSafety + 100)
	r.NoError(e)

	o.getActiveSet = func(epoch types.EpochId, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return createMapWithSize(19), nil
	}
	v2, e := o.activeSetSize(defSafety + 100)
	r.NoError(e)
	r.Equal(v1, v2)
}

func TestOracle_roundedSafeLayer(t *testing.T) {
	const offset = 3
	r := require.New(t)
	v := roundedSafeLayer(1, 1, 1, offset)
	r.Equal(config.Genesis, v)
	v = roundedSafeLayer(1, 5, 1, offset)
	r.Equal(config.Genesis, v)
	v = roundedSafeLayer(50, 5, 10, offset)
	r.Equal(types.LayerID(43), v)
	v = roundedSafeLayer(2, 1, 4, 2)
	r.Equal(config.Genesis, v)
	v = roundedSafeLayer(10, 1, 4, offset)
	r.Equal(types.LayerID(4+offset), v)

	// examples
	// sl is after rounded layer
	v = roundedSafeLayer(11, 5, 5, 1)
	r.Equal(types.LayerID(6), v)
	// sl is before rounded layer
	v = roundedSafeLayer(11, 5, 5, 3)
	r.Equal(types.LayerID(3), v)
}

func TestOracle_actives(t *testing.T) {
	r := require.New(t)
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, 5, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	_, err := o.actives(1)
	r.EqualError(err, errGenesis.Error())

	o.blocksProvider = mockBlocksProvider{mp: make(map[types.BlockID]struct{})}
	_, err = o.actives(100)
	r.EqualError(err, errNoContextualBlocks.Error())

	o.blocksProvider = mockBlocksProvider{}
	mp := createMapWithSize(9)
	o.getActiveSet = func(epoch types.EpochId, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return mp, nil
	}
	o.activesCache = newMockCasher()
	v, err := o.actives(100)
	r.NoError(err)
	v2, err := o.actives(100)
	r.NoError(err)
	r.Equal(v, v2)
	for k := range mp {
		_, exist := v[k]
		r.True(exist)
	}

	o.getActiveSet = func(epoch types.EpochId, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return createMapWithSize(9), someErr
	}
	_, err = o.actives(200)
	r.Equal(someErr, err)
}

func TestOracle_concurrentActives(t *testing.T) {
	r := require.New(t)
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, 5, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))

	mc := newMockCasher()
	o.activesCache = mc
	mp := createMapWithSize(9)
	o.getActiveSet = func(epoch types.EpochId, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return mp, nil
	}

	// outstanding probability for concurrent access to calc active set size
	for i := 0; i < 100; i++ {
		go o.actives(100)
	}

	// make sure we wait at least two calls duration
	o.actives(100)
	o.actives(100)

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
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, 2, genActive, mockBlocksProvider{}, eCfg.Config{ConfidenceParam: 2, EpochOffset: 0}, log.NewDefault(t.Name()))
	mp := createMapWithSize(9)
	o.activesCache = newMockCasher()
	lyr := types.LayerID(10)
	rsl := roundedSafeLayer(lyr, types.LayerID(o.cfg.ConfidenceParam), o.layersPerEpoch, types.LayerID(o.cfg.EpochOffset))
	o.getActiveSet = func(epoch types.EpochId, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		ep := rsl.GetEpoch(o.layersPerEpoch)
		r.Equal(ep, epoch)
		return mp, nil
	}

	bmp := make(map[types.LayerID]map[types.BlockID]struct{})
	mp2 := make(map[types.BlockID]struct{})
	block1 := types.NewExistingBlock(0, []byte("some data"))
	mp2[block1.Id()] = struct{}{}
	bmp[rsl] = mp2
	o.blocksProvider = &bProvider{bmp}
	mpRes, err := o.actives(lyr)
	r.NotNil(mpRes)
	r.NoError(err)
}

func TestOracle_IsIdentityActive(t *testing.T) {
	r := require.New(t)
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, 5, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	mp := make(map[string]struct{})
	edid := "11111"
	mp[edid] = struct{}{}
	o.getActiveSet = func(epoch types.EpochId, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return mp, nil
	}
	v, err := o.IsIdentityActiveOnConsensusView("22222", 1)
	r.NoError(err)
	r.True(v)

	o.getActiveSet = func(epoch types.EpochId, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return mp, someErr
	}
	_, err = o.IsIdentityActiveOnConsensusView("22222", 100)
	r.Equal(someErr, err)

	o.getActiveSet = func(epoch types.EpochId, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return mp, nil
	}

	v, err = o.IsIdentityActiveOnConsensusView("22222", 100)
	r.NoError(err)
	r.False(v)

	v, err = o.IsIdentityActiveOnConsensusView(edid, 100)
	r.NoError(err)
	r.True(v)
}

func TestOracle_Eligible2(t *testing.T) {
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, 5, genActive, mockBlocksProvider{}, cfg, log.NewDefault(t.Name()))
	o.getActiveSet = func(epoch types.EpochId, blocks map[types.BlockID]struct{}) (map[string]struct{}, error) {
		return createMapWithSize(9), someErr
	}
	o.vrfVerifier = func(msg, sig, pub []byte) (bool, error) {
		return true, nil
	}
	_, err := o.Eligible(100, 1, 1, types.NodeId{}, []byte{})
	assert.Equal(t, someErr, err)
}
