package eligibility

import (
	"errors"
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
	err  error
}

func (m *mockActiveSetProvider) GetActiveSetSize(layer types.LayerID) (uint32, error) {
	return m.size, m.err
}

func buildVerifier(result bool, err error) VerifierFunc {
	return func(msg, sig []byte, pub []byte) (bool, error) {
		return result, err
	}
}

func TestOracle_BuildVRFMessage(t *testing.T) {
	o := Oracle{}
	o.beacon = &mockValueProvider{1, someErr}
	_, err := o.buildVRFMessage(types.NodeId{}, types.LayerID(1), 1)
	assert.NotNil(t, err)
}

func TestOracle_IsEligible(t *testing.T) {
	o := &Oracle{beacon: &mockValueProvider{1, nil}}
	o.vrf = buildVerifier(false, someErr)
	res, err := o.Eligible(types.LayerID(1), 0, 1, types.NodeId{}, []byte{})
	assert.NotNil(t, err)
	assert.False(t, res)

	o.vrf = buildVerifier(true, nil)
	o.activeSetProvider = &mockActiveSetProvider{5, someErr}
	res, err = o.Eligible(types.LayerID(1), 1, 0, types.NodeId{}, []byte{})
	assert.NotNil(t, err)
	assert.False(t, res)

	o.activeSetProvider = &mockActiveSetProvider{10, nil}
	res, err = o.Eligible(types.LayerID(1), 1, 0, types.NodeId{}, []byte{})
	assert.Nil(t, err)
	assert.False(t, res)

	o.activeSetProvider = &mockActiveSetProvider{0, nil}
	res, err = o.Eligible(types.LayerID(1), 1, 0, types.NodeId{}, []byte{})
	assert.NotNil(t, err)
	assert.Equal(t, "active set size is zero", err.Error())
	assert.False(t, res)

	o.activeSetProvider = &mockActiveSetProvider{10, nil}
	res, err = o.Eligible(types.LayerID(1), 1, 10, types.NodeId{}, []byte{})
	assert.Nil(t, err)
	assert.True(t, res)
}

func Test_safeLayer(t *testing.T) {
	assert.Equal(t, config.Genesis, safeLayer(1))
	assert.Equal(t, 100-k, safeLayer(100))
}

func Test_ZeroParticipants(t *testing.T) {
	o := New(&mockValueProvider{1, nil}, &mockActiveSetProvider{5, nil}, buildVerifier(true, nil))
	res, err := o.Eligible(0, 0, 0, types.NodeId{Key: ""}, []byte{1})
	assert.Nil(t, err)
	assert.False(t, res)
}

func Test_AllParticipants(t *testing.T) {
	o := New(&mockValueProvider{1, nil}, &mockActiveSetProvider{5, nil}, buildVerifier(true, nil))
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
	o := New(&mockValueProvider{1, nil}, &mockActiveSetProvider{setSize, nil}, buildVerifier(true, nil))
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
