package sync

import (
	"github.com/spacemeshos/go-spacemesh/block"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"math/big"
)

type BlockValidatorMock struct {
}

func (BlockValidatorMock) BlockEligible(layerID block.LayerID, nodeID block.NodeId, proof block.BlockEligibilityProof) (bool, error) {
	return true, nil
}

type MeshValidatorMock struct{}

func (m *MeshValidatorMock) HandleLateBlock(bl *block.Block)              {}
func (m *MeshValidatorMock) RegisterLayerCallback(func(id block.LayerID)) {}
func (mlg *MeshValidatorMock) ContextualValidity(id block.BlockID) bool   { return true }

func (m *MeshValidatorMock) HandleIncomingLayer(layer *block.Layer) (block.LayerID, block.LayerID) {
	return layer.Index() - 1, layer.Index()
}

type StateMock struct{}

func (s *StateMock) ApplyTransactions(id block.LayerID, tx mesh.Transactions) (uint32, error) {
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

func (MockState) ApplyTransactions(layer block.LayerID, txs mesh.Transactions) (uint32, error) {
	return 0, nil
}

func (MockState) ApplyRewards(layer block.LayerID, miners map[string]struct{}, underQuota map[string]struct{}, bonusReward, diminishedReward *big.Int) {
}

func (s *StateMock) ApplyRewards(layer block.LayerID, miners map[string]struct{}, underQuota map[string]struct{}, bonusReward, diminishedReward *big.Int) {

}

type AtxDbMock struct {}

func (AtxDbMock) ProcessBlockATXs(block *block.Block) {

}
