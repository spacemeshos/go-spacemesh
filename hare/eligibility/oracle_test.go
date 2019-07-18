package eligibility

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/amcl/BLS381"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math/rand"
	"strconv"
	"testing"
	"time"
)

var someErr = errors.New("some error")
var myErr = errors.New("my error")

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

func (m *mockActiveSetProvider) ActiveSetSize(id types.LayerID, layersPerEpoch uint16) (map[string]types.AtxId, error) {
	return createMapWithSize(m.size), nil
}

func buildVerifier(result bool, err error) VerifierFunc {
	return func(msg, sig []byte, pub []byte) (bool, error) {
		return result, err
	}
}

type signer struct {
	sig []byte
	err error
}

func (s *signer) Sign(msg []byte) ([]byte, error) {
	return s.sig, s.err
}

func TestOracle_BuildVRFMessage(t *testing.T) {
	o := Oracle{Log: log.NewDefault(t.Name())}
	o.beacon = &mockValueProvider{1, someErr}
	_, err := o.buildVRFMessage(types.NodeId{}, types.LayerID(1), 1)
	assert.NotNil(t, err)
}

func TestOracle_IsEligible(t *testing.T) {
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, 0, log.NewDefault(t.Name()))
	o.layersPerEpoch = 10
	o.vrfVerifier = buildVerifier(false, someErr)
	res, err := o.Eligible(types.LayerID(1), 0, 1, types.NodeId{}, []byte{})
	assert.NotNil(t, err)
	assert.False(t, res)

	o.vrfVerifier = buildVerifier(true, nil)
	o.getActiveSet = (&mockActiveSetProvider{10}).ActiveSetSize
	res, err = o.Eligible(types.LayerID(1), 1, 0, types.NodeId{}, []byte{})
	assert.Nil(t, err)
	assert.False(t, res)

	o.getActiveSet = (&mockActiveSetProvider{0}).ActiveSetSize
	res, err = o.Eligible(types.LayerID(k+11), 1, 0, types.NodeId{}, []byte{})
	assert.NotNil(t, err)
	assert.Equal(t, "active set size is zero", err.Error())
	assert.False(t, res)

	o.getActiveSet = (&mockActiveSetProvider{10}).ActiveSetSize
	res, err = o.Eligible(types.LayerID(1), 1, 10, types.NodeId{}, []byte{})
	assert.Nil(t, err)
	assert.True(t, res)
}

func Test_safeLayer(t *testing.T) {
	assert.Equal(t, config.Genesis, safeLayer(1))
	assert.Equal(t, 100-k, safeLayer(100))
}

func Test_ZeroParticipants(t *testing.T) {
	o := New(&mockValueProvider{1, nil}, (&mockActiveSetProvider{5}).ActiveSetSize, buildVerifier(true, nil), &signer{}, 10, log.NewDefault(t.Name()))
	res, err := o.Eligible(0, 0, 0, types.NodeId{Key: ""}, []byte{1})
	assert.Nil(t, err)
	assert.False(t, res)
}

func Test_AllParticipants(t *testing.T) {
	o := New(&mockValueProvider{1, nil}, (&mockActiveSetProvider{5}).ActiveSetSize, buildVerifier(true, nil), &signer{}, 10, log.NewDefault(t.Name()))
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
	o := New(&mockValueProvider{1, nil}, (&mockActiveSetProvider{setSize}).ActiveSetSize, buildVerifier(true, nil), &signer{}, 10, log.NewDefault(t.Name()))
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
	size map[types.LayerID]int
}

func (m *mockBufferedActiveSetProvider) ActiveSet(id types.LayerID, layersPerEpoch uint16) (map[string]types.AtxId, error) {
	v, ok := m.size[id]
	if !ok {
		return createMapWithSize(0), errors.New("no instance")
	}

	return createMapWithSize(v), nil
}

func createMapWithSize(n int) map[string]types.AtxId {
	m := make(map[string]types.AtxId)
	for i := 0; i < n; i++ {
		m[strconv.Itoa(i)] = *types.EmptyAtxId
	}

	return m
}

func Test_ActiveSetSize(t *testing.T) {
	m := make(map[types.LayerID]int)
	m[types.LayerID(19)] = 2
	m[types.LayerID(29)] = 3
	m[types.LayerID(39)] = 5
	o := New(&mockValueProvider{1, nil}, (&mockBufferedActiveSetProvider{m}).ActiveSet, buildVerifier(true, nil), &signer{}, 10, log.NewDefault(t.Name()))
	// TODO: remove this comment after inception problem is addressed
	//assert.Equal(t, o.getActiveSet.ActiveSet(0), o.activeSetSize(1))
	l := 19 + k
	assertActiveSetSize(t, o, 2, l)
	assertActiveSetSize(t, o, 3, l+10)
	assertActiveSetSize(t, o, 5, l+20)

	// create error
	o.getActiveSet = func(layer types.LayerID, layersPerEpoch uint16) (map[string]types.AtxId, error) {

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
	o := New(&mockValueProvider{1, nil}, (&mockActiveSetProvider{10}).ActiveSetSize, BLS381.Verify2, sr, 10, log.NewDefault(t.Name()))
	id := types.NodeId{Key: "abc", VRFPublicKey: pu}
	proof, err := o.Proof(id, 1, 1)
	assert.Nil(t, err)
	res, err := o.Eligible(1, 1, 10, id, proof)
	assert.Nil(t, err)
	assert.True(t, res)
}

func TestOracle_Proof(t *testing.T) {
	o := New(&mockValueProvider{0, myErr}, (&mockActiveSetProvider{10}).ActiveSetSize, buildVerifier(true, nil), &signer{}, 10, log.NewDefault(t.Name()))
	sig, err := o.Proof(types.NodeId{}, 2, 3)
	assert.Nil(t, sig)
	assert.NotNil(t, err)
	assert.Equal(t, myErr, err)
	o.beacon = &mockValueProvider{0, nil}
	o.vrfSigner = &signer{nil, myErr}
	sig, err = o.Proof(types.NodeId{}, 2, 3)
	assert.Nil(t, sig)
	assert.NotNil(t, err)
	assert.Equal(t, myErr, err)
	mySig := []byte{1, 2}
	o.vrfSigner = &signer{mySig, nil}
	sig, err = o.Proof(types.NodeId{}, 2, 3)
	assert.Nil(t, err)
	assert.Equal(t, mySig, sig)
}

func TestOracle_Eligible(t *testing.T) {
	o := New(&mockValueProvider{0, myErr}, (&mockActiveSetProvider{10}).ActiveSetSize, buildVerifier(true, nil), &signer{}, 10, log.NewDefault(t.Name()))
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
	o := New(&mockValueProvider{1, nil}, nil, nil, nil, 5, log.NewDefault(t.Name()))
	o.getActiveSet = func(layer types.LayerID, layersPerEpoch uint16) (map[string]types.AtxId, error) {
		return createMapWithSize(17), nil
	}
	v1, e := o.activeSetSize(k + 100)
	r.NoError(e)

	o.getActiveSet = func(layer types.LayerID, layersPerEpoch uint16) (map[string]types.AtxId, error) {
		return createMapWithSize(19), nil
	}
	v2, e := o.activeSetSize(k + 100)
	r.NoError(e)
	r.Equal(v1, v2)
}
