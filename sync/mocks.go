package sync

import (
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/types"
	"math/big"
)

type BlockValidatorMock struct {
}

func (BlockValidatorMock) BlockEligible(block *types.BlockHeader) (bool, error) {
	return true, nil
}

type TxValidatorMock struct {
}

func (TxValidatorMock) TxValid(tx types.SerializableTransaction) bool {
	return true
}

type MeshValidatorMock struct{}

func (m *MeshValidatorMock) HandleLateBlock(bl *types.Block)              {}
func (m *MeshValidatorMock) RegisterLayerCallback(func(id types.LayerID)) {}
func (mlg *MeshValidatorMock) ContextualValidity(id types.BlockID) bool   { return true }

func (m *MeshValidatorMock) HandleIncomingLayer(layer *types.Layer) (types.LayerID, types.LayerID) {
	return layer.Index() - 1, layer.Index()
}

type StateMock struct{}

func (s *StateMock) ApplyTransactions(id types.LayerID, tx mesh.Transactions) (uint32, error) {
	return 0, nil
}

func ConfigTst() mesh.Config {
	return mesh.Config{
		SimpleTxCost:   big.NewInt(10),
		BaseReward:     big.NewInt(5000),
		PenaltyPercent: big.NewInt(15),
		TxQuota:        15,
		RewardMaturity: 5,
	}
}

type MockState struct{}

func (MockState) ApplyTransactions(layer types.LayerID, txs mesh.Transactions) (uint32, error) {
	return 0, nil
}

func (MockState) ApplyRewards(layer types.LayerID, miners []string, underQuota map[string]int, bonusReward, diminishedReward *big.Int) {
}

func (s *StateMock) ApplyRewards(layer types.LayerID, miners []string, underQuota map[string]int, bonusReward, diminishedReward *big.Int) {

}

type AtxDbMock struct{}

func (AtxDbMock) GetAtx(id types.AtxId) (*types.ActivationTx, error) {
	return nil, nil
}

func (AtxDbMock) ProcessBlockATXs(block *types.Block) {

}

type MockIStore struct {
}

func (*MockIStore) StoreNodeIdentity(id types.NodeId) error {
	return nil
}

func (*MockIStore) GetIdentity(id string) (types.NodeId, error) {
	return types.NodeId{}, nil
}

type ValidatorMock struct{}

func (*ValidatorMock) Validate(nipst *nipst.NIPST, expectedChallenge common.Hash) error {
	return nil
}
