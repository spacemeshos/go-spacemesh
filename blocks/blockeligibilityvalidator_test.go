package blocks

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

var errFoo = errors.New("some err")

type mockAtxDB struct {
	atxH *types.ActivationTxHeader
	err  error
}

func (m mockAtxDB) GetEpochAtxs(types.EpochID) []types.ATXID {
	return []types.ATXID{}
}

func (m mockAtxDB) GetIdentity(edID string) (types.NodeID, error) {
	return types.NodeID{Key: edID, VRFPublicKey: nodeID.VRFPublicKey}, nil
}

func (m mockAtxDB) GetNodeAtxIDForEpoch(types.NodeID, types.EpochID) (types.ATXID, error) {
	return types.ATXID{}, m.err
}

func (m mockAtxDB) GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error) {
	return m.atxH, m.err
}

func (m mockAtxDB) GetEpochWeight(types.EpochID) (uint64, []types.ATXID, error) {
	return 0, nil, nil
}

func TestBlockEligibilityValidator_getValidAtx(t *testing.T) {
	types.SetLayersPerEpoch(5)
	r := require.New(t)
	atxdb := &mockAtxDB{err: errFoo}
	v := NewBlockEligibilityValidator(10, 5, 5, atxdb, &EpochBeaconProvider{}, validateVRF, nil, log.NewDefault(t.Name()))

	block := &types.Block{MiniBlock: types.MiniBlock{BlockHeader: types.BlockHeader{LayerIndex: 20}}} // non-genesis
	block.Signature = edSigner.Sign(block.Bytes())
	block.Initialize()
	_, err := v.getValidAtx(block)
	r.EqualError(err, "getting ATX failed: some err 0000000000 ep(4)")

	v.activationDb = &mockAtxDB{atxH: &types.ActivationTxHeader{}} // not same epoch
	_, err = v.getValidAtx(block)
	r.EqualError(err, "ATX target epoch (1) doesn't match block publication epoch (4)")

	atxHeader := &types.ActivationTxHeader{NIPSTChallenge: types.NIPSTChallenge{
		NodeID:     types.NodeID{Key: edSigner.PublicKey().String()},
		PubLayerID: 18,
	}}
	v.activationDb = &mockAtxDB{atxH: atxHeader}
	atx, err := v.getValidAtx(block)
	r.NoError(err)
	r.Equal(atxHeader, atx)
}
