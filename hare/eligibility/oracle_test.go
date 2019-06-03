package eligibility

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/amcl/BLS381"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"math/rand"
	"testing"
	"time"
)

var someErr = errors.New("some error")

type mockValueProvider struct {
	val uint32
	err error
}

func (mvp *mockValueProvider) Value(layer types.LayerID) (uint32, error) {
	return mvp.val, mvp.err
}

type mockActiveSetProvider struct {
	size uint32
}

func (m *mockActiveSetProvider) ActiveSetSize(eph types.EpochId) uint32 {
	return m.size
}

func buildVerifier(result bool, err error) VerifierFunc {
	return func(msg, sig []byte, pub []byte) (bool, error) {
		return result, err
	}
}

type signer struct {
}

func (*signer) Sign(msg []byte) ([]byte, error) {
	return []byte{}, nil
}

func TestOracle_BuildVRFMessage(t *testing.T) {
	o := Oracle{}
	o.beacon = &mockValueProvider{1, someErr}
	_, err := o.buildVRFMessage(types.NodeId{}, types.LayerID(1), 1)
	assert.NotNil(t, err)
}

func TestOracle_IsEligible(t *testing.T) {
	o := &Oracle{beacon: &mockValueProvider{1, nil}}
	o.layersPerEpoch = 10
	o.vrfVerifier = buildVerifier(false, someErr)
	res, err := o.Eligible(types.LayerID(1), 0, 1, types.NodeId{}, []byte{})
	assert.NotNil(t, err)
	assert.False(t, res)

	o.vrfVerifier = buildVerifier(true, nil)
	o.activeSetProvider = &mockActiveSetProvider{10}
	res, err = o.Eligible(types.LayerID(1), 1, 0, types.NodeId{}, []byte{})
	assert.Nil(t, err)
	assert.False(t, res)

	o.activeSetProvider = &mockActiveSetProvider{0}
	res, err = o.Eligible(types.LayerID(k+11), 1, 0, types.NodeId{}, []byte{})
	assert.NotNil(t, err)
	assert.Equal(t, "active set size is zero", err.Error())
	assert.False(t, res)

	o.activeSetProvider = &mockActiveSetProvider{10}
	res, err = o.Eligible(types.LayerID(1), 1, 10, types.NodeId{}, []byte{})
	assert.Nil(t, err)
	assert.True(t, res)
}

func Test_safeLayer(t *testing.T) {
	assert.Equal(t, config.Genesis, safeLayer(1))
	assert.Equal(t, 100-k, safeLayer(100))
}

func Test_ZeroParticipants(t *testing.T) {
	o := New(&mockValueProvider{1, nil}, &mockActiveSetProvider{5}, buildVerifier(true, nil), &signer{}, 10)
	res, err := o.Eligible(0, 0, 0, types.NodeId{Key: ""}, []byte{1})
	assert.Nil(t, err)
	assert.False(t, res)
}

func Test_AllParticipants(t *testing.T) {
	o := New(&mockValueProvider{1, nil}, &mockActiveSetProvider{5}, buildVerifier(true, nil), &signer{}, 10)
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
	setSize := uint32(1024)
	commSize := 1000
	o := New(&mockValueProvider{1, nil}, &mockActiveSetProvider{setSize}, buildVerifier(true, nil),  &signer{},10)
	count := 0
	for i := uint32(0); i < setSize; i++ {
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
	size map[types.EpochId]uint32
}

func (m *mockBufferedActiveSetProvider) ActiveSetSize(eph types.EpochId) uint32 {
	v, ok := m.size[eph]
	if !ok {
		return 0
	}

	return v
}

func Test_ActiveSetSize(t *testing.T) {
	m := make(map[types.EpochId]uint32)
	m[types.EpochId(0)] = 2
	m[types.EpochId(1)] = 3
	m[types.EpochId(2)] = 5
	o := New(&mockValueProvider{1, nil}, &mockBufferedActiveSetProvider{m}, buildVerifier(true, nil), &signer{}, 10)
	// TODO: remove this comment after inception problem is addressed
	//assert.Equal(t, o.activeSetProvider.ActiveSetSize(0), o.activeSetSize(1))
	l := 19 + k
	assert.Equal(t, uint32(2), o.activeSetSize(l))
	assert.Equal(t, uint32(3), o.activeSetSize(l+10))
	assert.Equal(t, uint32(5), o.activeSetSize(l+20))
}

func Test_BlsSignVerify(t *testing.T) {
	pr, pu := BLS381.GenKeyPair()
	sr := BLS381.NewBlsSigner(pr)
	o := New(&mockValueProvider{1, nil}, &mockActiveSetProvider{10}, BLS381.Verify2, sr,10)
	id := types.NodeId{Key: "abc", VRFPublicKey: pu}
	proof, err := o.Proof(id, 1, 1)
	assert.Nil(t, err)
	res, err := o.Eligible(1, 1, 10, id, proof)
	assert.Nil(t, err)
	assert.True(t, res)
}