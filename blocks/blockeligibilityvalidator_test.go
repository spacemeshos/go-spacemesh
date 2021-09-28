package blocks

import (
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/blocks/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
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

func mockTortoiseBeacon(t *testing.T) BeaconGetter {
	ctrl := gomock.NewController(t)
	mockTB := mocks.NewMockBeaconGetter(ctrl)
	mockTB.EXPECT().GetBeacon(gomock.Any()).Return(types.HexToHash32("0x94812631").Bytes(), nil).AnyTimes()
	return mockTB
}

func TestBlockEligibilityValidator_getValidAtx(t *testing.T) {
	types.SetLayersPerEpoch(5)
	r := require.New(t)
	atxdb := &mockAtxDB{err: errFoo}
	v := NewBlockEligibilityValidator(10, 5, atxdb, mockTortoiseBeacon(t), validateVRF, nil, logtest.New(t))

	block := &types.Block{MiniBlock: types.MiniBlock{BlockHeader: types.BlockHeader{LayerIndex: types.NewLayerID(20)}}} // non-genesis
	block.Signature = edSigner.Sign(block.Bytes())
	block.Initialize()
	_, err := v.getValidAtx(block)
	r.EqualError(err, "getting ATX failed: some err 0000000000 ep(4)")

	v.activationDb = &mockAtxDB{atxH: &types.ActivationTxHeader{}} // not same epoch
	_, err = v.getValidAtx(block)
	r.EqualError(err, "ATX target epoch (1) doesn't match block publication epoch (4)")

	atxHeader := &types.ActivationTxHeader{NIPostChallenge: types.NIPostChallenge{
		NodeID:     types.NodeID{Key: edSigner.PublicKey().String()},
		PubLayerID: types.NewLayerID(18),
	}}
	v.activationDb = &mockAtxDB{atxH: atxHeader}
	atx, err := v.getValidAtx(block)
	r.NoError(err)
	r.Equal(atxHeader, atx)
}
