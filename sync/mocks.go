package sync

import (
	"github.com/spacemeshos/go-spacemesh/mesh"
	"math/big"
)

type BlockValidatorMock struct {
}

func (BlockValidatorMock) BlockEligible(id mesh.LayerID, key string) bool {
	return true
}

type MeshValidatorMock struct{}

func (m *MeshValidatorMock) HandleIncomingLayer(layer *mesh.Layer) (mesh.LayerID, mesh.LayerID) {
	return layer.Index() - 1, layer.Index()
}
func (m *MeshValidatorMock) HandleLateBlock(bl *mesh.Block)              {}
func (m *MeshValidatorMock) RegisterLayerCallback(func(id mesh.LayerID)) {}
func (mlg *MeshValidatorMock) ContextualValidity(id mesh.BlockID) bool   { return true }

type StateMock struct{}

func (s *StateMock) ApplyTransactions(id mesh.LayerID, tx mesh.Transactions) (uint32, error) {
	return 0, nil
}

func ConfigTst() mesh.RewardConfig {
	return mesh.RewardConfig{
		SimpleTxCost:   big.NewInt(10),
		BaseReward:     big.NewInt(5000),
		PenaltyPercent: big.NewInt(15),
		TxQuota:        15,
		RewardMaturity: 5,
	}
}

type MockState struct{}

func (MockState) ApplyTransactions(layer mesh.LayerID, txs mesh.Transactions) (uint32, error) {
	return 0, nil
}

func (MockState) ApplyRewards(layer mesh.LayerID, miners map[string]struct{}, underQuota map[string]struct{}, bonusReward, diminishedReward *big.Int) {
}

func (s *StateMock) ApplyRewards(layer mesh.LayerID, miners map[string]struct{}, underQuota map[string]struct{}, bonusReward, diminishedReward *big.Int) {

}
