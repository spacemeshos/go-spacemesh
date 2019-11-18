package oracle

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/require"
	"testing"
)

var someErr = errors.New("some err")

type mockAtxDB struct {
	atxH *types.ActivationTxHeader
	err  error
}

func (m *mockAtxDB) IsIdentityActive(edId string, layer types.LayerID) (*types.NodeId, bool, types.AtxId, error) {
	return &types.NodeId{}, false, types.AtxId{}, m.err
}

func (m mockAtxDB) GetIdentity(edId string) (types.NodeId, error) {
	return types.NodeId{Key: edId, VRFPublicKey: publicKey}, nil
}

func (m mockAtxDB) GetNodeLastAtxId(node types.NodeId) (types.AtxId, error) {
	return types.AtxId{}, m.err
}

func (m mockAtxDB) GetAtxHeader(id types.AtxId) (*types.ActivationTxHeader, error) {
	return m.atxH, m.err
}

func TestBlockEligibilityValidator_getActiveSet(t *testing.T) {
	r := require.New(t)
	atxdb := &mockAtxDB{err: someErr}
	genActiveSetSize := uint32(5)
	v := NewBlockEligibilityValidator(10, genActiveSetSize, 5, atxdb, &EpochBeaconProvider{},
		validateVrf, log.NewDefault(t.Name()))
	genBlock := &types.BlockHeader{LayerIndex: 0}
	val, err := v.getActiveSetSize(genBlock)
	r.NoError(err)
	r.Equal(genActiveSetSize, val)

	otherBlock := &types.BlockHeader{LayerIndex: 20} // non-genesis
	_, err = v.getActiveSetSize(otherBlock)
	r.Error(err)

	v.activationDb = &mockAtxDB{atxH: &types.ActivationTxHeader{}} // not same epoch
	_, err = v.getActiveSetSize(otherBlock)
	r.Error(err)

	v.activationDb = &mockAtxDB{atxH: &types.ActivationTxHeader{ActiveSetSize: 7, NIPSTChallenge: types.NIPSTChallenge{PubLayerIdx: 18}}}
	val, err = v.getActiveSetSize(otherBlock)
	r.NoError(err)
	r.Equal(uint32(7), val)
}
