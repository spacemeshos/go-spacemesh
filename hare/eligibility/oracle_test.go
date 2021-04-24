package eligibility

import (
	"context"
	"errors"
	"fmt"
	"github.com/spacemeshos/amcl/BLS381"
	"github.com/spacemeshos/go-spacemesh/common/types"
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

const defLayersPerEpoch = 10

var errFoo = errors.New("some error")
var errMy = errors.New("my error")
var cfg = eCfg.Config{ConfidenceParam: 25, EpochOffset: 30}
var genActive = 5
var hDist = 10

type mockValueProvider struct {
	val uint32
	err error
}

func (mvp *mockValueProvider) Value(layer types.LayerID) (uint32, error) {
	return mvp.val, mvp.err
}

type mockActiveSetProvider struct {
	size           int
	getEpochAtxsFn func(types.EpochID) []types.ATXID
	getAtxHeaderFn func(types.ATXID) (*types.ActivationTxHeader, error)
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

func (m *mockBufferedActiveSetProvider) GetEpochAtxs(epoch types.EpochID) []types.ATXID {
	v, ok := m.size[epoch]
	if !ok {
		return []types.ATXID{}
	}

	atxList := make([]types.ATXID, v)
	for i := 0; i < v; i++ {
		atxList[i] = types.ATXID{}
	}
	return atxList
}

func (m *mockBufferedActiveSetProvider) GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error) {
	return &types.ActivationTxHeader{NIPSTChallenge: types.NIPSTChallenge{NodeID: types.NodeID{Key: "fakekey"}}}, nil
}

func createMapWithSize(n int) map[string]struct{} {
	m := make(map[string]struct{})
	for i := 0; i < n; i++ {
		m[strconv.Itoa(i)] = struct{}{}
	}

	return m
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
	o := New(&mockValueProvider{1, nil}, &mockActiveSetProvider{size: 10}, buildVerifier(true, nil), &mockSigner{[]byte{1, 2, 3}, nil}, 5, 5, hDist, cfg, log.NewDefault(t.Name()))
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
	atxdb := &mockActiveSetProvider{size: 10}
	o := New(&mockValueProvider{1, nil}, atxdb, nil, nil, 0, genActive, hDist, cfg, log.NewDefault(t.Name()))
	o.layersPerEpoch = 10

	// VRF is ineligible
	o.vrfVerifier = buildVerifier(false, errFoo)
	res, err := o.Eligible(context.TODO(), types.LayerID(1), 0, 1, types.NodeID{}, []byte{})
	require.Error(t, err)
	require.False(t, res)

	// VRF eligible but zero committee size
	o.vrfVerifier = buildVerifier(true, nil)
	res, err = o.Eligible(context.TODO(), types.LayerID(50), 1, 0, types.NodeID{}, []byte{})
	require.NoError(t, err)
	require.False(t, res)

	// VRF eligible but empty active set
	o.atxdb = &mockActiveSetProvider{size: 0}
	res, err = o.Eligible(context.TODO(), types.LayerID(cfg.ConfidenceParam*2+11), 1, 0, types.NodeID{}, []byte{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty active set")
	require.False(t, res)

	// eligible
	o.atxdb = atxdb
	res, err = o.Eligible(context.TODO(), types.LayerID(50), 1, 10, types.NodeID{}, []byte{})
	require.NoError(t, err)
	require.True(t, res)
}

func Test_safeLayer(t *testing.T) {
	const safety = 25
	assert.Equal(t, types.GetEffectiveGenesis(), safeLayer(1, safety))
	assert.Equal(t, types.LayerID(100-safety), safeLayer(100, safety))
}

func Test_ZeroParticipants(t *testing.T) {
	o := New(&mockValueProvider{1, nil}, &mockActiveSetProvider{size: 5}, buildVerifier(true, nil), &mockSigner{}, defLayersPerEpoch, genActive, hDist, cfg, log.NewDefault(t.Name()))
	res, err := o.Eligible(context.TODO(), 1, 0, 0, types.NodeID{Key: ""}, []byte{1})
	assert.Nil(t, err)
	assert.False(t, res)
}

func Test_AllParticipants(t *testing.T) {
	o := New(&mockValueProvider{1, nil}, &mockActiveSetProvider{size: 5}, buildVerifier(true, nil), &mockSigner{}, 10, genActive, hDist, cfg, log.NewDefault(t.Name()))
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
	o := New(&mockValueProvider{1, nil}, &mockActiveSetProvider{size: setSize}, buildVerifier(true, nil), &mockSigner{}, 10, genActive, hDist, cfg, log.NewDefault(t.Name()))
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

func Test_ActiveSetSize(t *testing.T) {
	t.Skip()
	m := make(map[types.EpochID]int)
	m[types.EpochID(19)] = 2
	m[types.EpochID(29)] = 3
	m[types.EpochID(39)] = 5
	o := New(&mockValueProvider{1, nil}, &mockBufferedActiveSetProvider{m}, buildVerifier(true, nil), &mockSigner{}, 10, genActive, hDist, cfg, log.NewDefault(t.Name()))
	// TODO: remove this comment after inception problem is addressed
	//assert.Equal(t, o.getActiveSet.ActiveSet(0), o.activeSetSize(1))
	l := types.LayerID(19)
	assertActiveSetSize(t, o, 2, l)
	assertActiveSetSize(t, o, 3, l+10)
	assertActiveSetSize(t, o, 5, l+20)

	// create error
	o.atxdb = &mockActiveSetProvider{getAtxHeaderFn: func(types.ATXID) (*types.ActivationTxHeader, error) {
		return nil, errFoo
	}}
	activeSetSize, err := o.activeSetSize(l + 19)
	assert.Error(t, err)
	assert.Equal(t, errFoo, err)
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
	o := New(&mockValueProvider{1, nil}, &mockActiveSetProvider{size: 10}, BLS381.Verify2, sr, 10, genActive, hDist, cfg, log.NewDefault(t.Name()))
	id := types.NodeID{Key: "abc", VRFPublicKey: pu}
	proof, err := o.Proof(context.TODO(), 1, 1)
	assert.Nil(t, err)
	res, err := o.Eligible(context.TODO(), 1, 1, 10, id, proof)
	assert.Nil(t, err)
	assert.True(t, res)
}

func TestOracle_Proof(t *testing.T) {
	o := New(&mockValueProvider{0, errMy}, &mockActiveSetProvider{size: 10}, buildVerifier(true, nil), &mockSigner{}, 10, genActive, hDist, cfg, log.NewDefault(t.Name()))
	sig, err := o.Proof(context.TODO(), 2, 3)
	assert.Nil(t, sig)
	assert.NotNil(t, err)
	assert.Equal(t, errMy, err)
	o.beacon = &mockValueProvider{0, nil}
	o.vrfSigner = &mockSigner{nil, errMy}
	sig, err = o.Proof(context.TODO(), 2, 3)
	assert.Nil(t, sig)
	assert.NotNil(t, err)
	assert.Equal(t, errMy, err)
	mySig := []byte{1, 2}
	o.vrfSigner = &mockSigner{mySig, nil}
	sig, err = o.Proof(context.TODO(), 2, 3)
	assert.Nil(t, err)
	assert.Equal(t, mySig, sig)
}

func TestOracle_Eligible(t *testing.T) {
	o := New(&mockValueProvider{0, errMy}, &mockActiveSetProvider{size: 10}, buildVerifier(true, nil), &mockSigner{}, 10, genActive, hDist, cfg, log.NewDefault(t.Name()))
	res, err := o.Eligible(context.TODO(), 1, 2, 3, types.NodeID{}, []byte{})
	assert.False(t, res)
	assert.NotNil(t, err)
	assert.Equal(t, errMy, err)

	o.beacon = &mockValueProvider{0, nil}
	o.vrfVerifier = buildVerifier(false, nil)
	res, err = o.Eligible(context.TODO(), 1, 2, 3, types.NodeID{}, []byte{})
	assert.False(t, res)
	assert.Nil(t, err)
}

func TestOracle_activeSetSizeCache(t *testing.T) {
	r := require.New(t)
	atxdb1 := &mockActiveSetProvider{size: 17}
	atxdb2 := &mockActiveSetProvider{size: 19}
	o := New(&mockValueProvider{1, nil}, atxdb1, nil, nil, 5, genActive, hDist, cfg, log.NewDefault(t.Name()))
	v1, e := o.activeSetSize(100)
	r.NoError(e)

	o.atxdb = atxdb2
	v2, e := o.activeSetSize(100)
	r.NoError(e)
	r.Equal(v1, v2)
}

func TestOracle_actives(t *testing.T) {
	r := require.New(t)
	n := 9
	atxdb := &mockActiveSetProvider{
		size: 0,
		getEpochAtxsFn: func(types.EpochID) []types.ATXID {
			atxids := make([]types.ATXID, n)
			for i := 0; i < n; i++ {
				atxids[i] = types.ATXID(types.BytesToHash([]byte{byte(i)}))
			}
			return atxids
		},
		getAtxHeaderFn: func(atxid types.ATXID) (*types.ActivationTxHeader, error) {
			return &types.ActivationTxHeader{NIPSTChallenge: types.NIPSTChallenge{NodeID: types.NodeID{Key: atxid.ShortString()}}}, nil
		},
	}
	o := New(&mockValueProvider{1, nil}, atxdb, nil, nil, 5, genActive, hDist, cfg, log.NewDefault(t.Name()))
	_, err := o.actives(1)
	r.EqualError(err, errGenesis.Error())

	o.activesCache = newMockCacher()
	v, err := o.actives(100)
	r.NoError(err)
	v2, err := o.actives(100)
	r.NoError(err)
	r.Equal(v, v2)
	for i := 0; i < n; i++ {
		_, exist := v[fmt.Sprintf("%x", i)]
		r.True(exist)
	}
}

func TestOracle_concurrentActives(t *testing.T) {
	r := require.New(t)
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, 5, genActive, hDist, cfg, log.NewDefault(t.Name()))

	mc := newMockCacher()
	o.activesCache = mc

	// outstanding probability for concurrent access to calc active set size
	for i := 0; i < 100; i++ {
		go o.actives(100)
	}

	// make sure we wait at least two calls duration
	o.actives(100)
	o.actives(100)

	r.Equal(1, mc.numAdd)
}

func TestOracle_IsIdentityActive(t *testing.T) {
	r := require.New(t)
	types.SetLayersPerEpoch(10)
	edid := "11111"
	atxdb := &mockActiveSetProvider{size: 1, getAtxHeaderFn: func(types.ATXID) (*types.ActivationTxHeader, error) {
		return &types.ActivationTxHeader{NIPSTChallenge: types.NIPSTChallenge{NodeID: types.NodeID{Key: edid}}}, nil
	}}
	o := New(&mockValueProvider{1, nil}, atxdb, nil, nil, 5, genActive, hDist, cfg, log.NewDefault(t.Name()))
	v, err := o.IsIdentityActiveOnConsensusView("22222", 1)
	r.NoError(err)
	r.True(v)

	o.atxdb = &mockActiveSetProvider{size: 1, getAtxHeaderFn: func(types.ATXID) (*types.ActivationTxHeader, error) {
		return &types.ActivationTxHeader{NIPSTChallenge: types.NIPSTChallenge{NodeID: types.NodeID{Key: edid}}}, errFoo
	}}
	_, err = o.IsIdentityActiveOnConsensusView("22222", 100)
	r.Equal(errFoo, err)

	o.atxdb = &mockActiveSetProvider{size: 1, getAtxHeaderFn: func(types.ATXID) (*types.ActivationTxHeader, error) {
		return &types.ActivationTxHeader{NIPSTChallenge: types.NIPSTChallenge{NodeID: types.NodeID{Key: edid}}}, nil
	}}

	v, err = o.IsIdentityActiveOnConsensusView("22222", 100)
	r.NoError(err)
	r.False(v)

	v, err = o.IsIdentityActiveOnConsensusView(edid, 100)
	r.NoError(err)
	r.True(v)
}

func TestOracle_Eligible2(t *testing.T) {
	types.SetLayersPerEpoch(10)
	atxdb := &mockActiveSetProvider{size: 1, getAtxHeaderFn: func(types.ATXID) (*types.ActivationTxHeader, error) {
		return &types.ActivationTxHeader{NIPSTChallenge: types.NIPSTChallenge{NodeID: types.NodeID{Key: "foo"}}}, errFoo
	}}
	o := New(&mockValueProvider{1, nil}, atxdb, nil, nil, 5, genActive, hDist, cfg, log.NewDefault(t.Name()))
	o.vrfVerifier = func(msg, sig, pub []byte) (bool, error) {
		return true, nil
	}
	_, err := o.Eligible(context.TODO(), 100, 1, 1, types.NodeID{}, []byte{})
	assert.Equal(t, errFoo, err)
}
