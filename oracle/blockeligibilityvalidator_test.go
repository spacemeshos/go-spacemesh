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

func (m mockAtxDB) GetIdentity(edId string) (types.NodeId, error) {
	return types.NodeId{Key: edId, VRFPublicKey: vrfPubkey}, nil
}

func (m mockAtxDB) GetNodeAtxIDForEpoch(nodeId types.NodeId, targetEpoch types.EpochId) (types.AtxId, error) {
	return types.AtxId{}, m.err
}

func (m mockAtxDB) GetAtxHeader(id types.AtxId) (*types.ActivationTxHeader, error) {
	return m.atxH, m.err
}

func TestBlockEligibilityValidator_getValidAtx(t *testing.T) {
	r := require.New(t)
	atxdb := &mockAtxDB{err: someErr}
	genActiveSetSize := uint32(5)
	v := NewBlockEligibilityValidator(10, genActiveSetSize, 5, atxdb, &EpochBeaconProvider{},
		validateVrf, log.NewDefault(t.Name()))

	block := &types.Block{MiniBlock: types.MiniBlock{BlockHeader: types.BlockHeader{LayerIndex: 20}}} // non-genesis
	block.Signature = edSigner.Sign(block.Bytes())
	block.Initialize()
	_, err := v.getValidAtx(block)
	r.EqualError(err, "getting ATX failed: some err 00000 ep(4)")

	v.activationDb = &mockAtxDB{atxH: &types.ActivationTxHeader{}} // not same epoch
	_, err = v.getValidAtx(block)
	r.EqualError(err, "ATX target epoch (1) doesn't match block publication epoch (4)")

	atxHeader := &types.ActivationTxHeader{ActiveSetSize: 7, NIPSTChallenge: types.NIPSTChallenge{
		NodeId:      types.NodeId{Key: edSigner.PublicKey().String()},
		PubLayerIdx: 18,
	}}
	v.activationDb = &mockAtxDB{atxH: atxHeader}
	atx, err := v.getValidAtx(block)
	r.NoError(err)
	r.Equal(atxHeader, atx)
}
