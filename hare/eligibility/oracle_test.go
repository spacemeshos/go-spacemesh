package eligibility

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"testing"
)

var someErr = errors.New("some error")

type mockValueProvider struct {
	val int
	err error
}

func (mvp *mockValueProvider) Value(layer types.LayerID) (int, error) {
	return mvp.val, mvp.err
}

type mockActiveSetProvider struct {
	size uint32
	err  error
}

func (m *mockActiveSetProvider) GetActiveSetSize(layer types.LayerID) (uint32, error) {
	return m.size, m.err
}

type mockVerifier struct {
	result bool
	err    error
}

func (mv *mockVerifier) Verify(msg, sig []byte) (bool, error) {
	return mv.result, mv.err
}

func TestOracle_BuildVRFMessage(t *testing.T) {
	o := Oracle{}
	o.beacon = &mockValueProvider{1, someErr}
	_, err := o.BuildVRFMessage(types.NodeId{}, types.LayerID(1), 1)
	assert.NotNil(t, err)
}

func TestOracle_IsEligible(t *testing.T) {
	o := &Oracle{}
	o.vrf = &mockVerifier{false, someErr}
	res, err := o.IsEligible(types.NodeId{}, types.LayerID(1), []byte{}, []byte{})
	assert.NotNil(t, err)
	assert.False(t, res)

	o.vrf = &mockVerifier{true, nil}
	o.asProvider = &mockActiveSetProvider{5, someErr}
	res, err = o.IsEligible(types.NodeId{}, types.LayerID(1), []byte{}, []byte{})
	assert.NotNil(t, err)
	assert.False(t, res)

	o.committeeSize = 0
	o.asProvider = &mockActiveSetProvider{10, nil}
	res, err = o.IsEligible(types.NodeId{}, types.LayerID(1), []byte{}, []byte{})
	assert.NotNil(t, err)
	assert.Equal(t, "did not pass eligibility threshold", err.Error())
	assert.False(t, res)

	o.asProvider = &mockActiveSetProvider{0, nil}
	res, err = o.IsEligible(types.NodeId{}, types.LayerID(1), []byte{}, []byte{})
	assert.NotNil(t, err)
	assert.Equal(t, "active set size is zero", err.Error())
	assert.False(t, res)

	o.committeeSize = 10
	o.asProvider = &mockActiveSetProvider{10, nil}
	res, err = o.IsEligible(types.NodeId{}, types.LayerID(1), []byte{}, []byte{})
	assert.Nil(t, err)
	assert.True(t, res)
}

func Test_safeLayer(t *testing.T) {
	assert.Equal(t, genesisLayer, safeLayer(1))
	assert.Equal(t, 100-k, safeLayer(100))
}
