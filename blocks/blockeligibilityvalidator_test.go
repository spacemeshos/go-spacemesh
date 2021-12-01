package blocks

import (
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/blocks/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

var errFoo = errors.New("some err")

type mockAtxDB struct {
	atxH *types.ActivationTxHeader
	err  error
}

func (m mockAtxDB) GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error) {
	return m.atxH, m.err
}

func TestBlockEligibilityValidator_getValidAtx(t *testing.T) {
	types.SetLayersPerEpoch(5)
	edSigner := signing.NewEdSigner()
	r := require.New(t)
	atxdb := &mockAtxDB{err: errFoo}
	mockBC := mocks.NewMockbeaconCollector(gomock.NewController(t))
	mockBC.EXPECT().ReportBeaconFromBlock(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
	v := NewBlockEligibilityValidator(10, 5, atxdb, mockBC, signing.VRFVerify, nil, logtest.New(t))

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
