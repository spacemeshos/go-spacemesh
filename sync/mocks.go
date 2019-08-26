package sync

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"math/big"
)

type PoetDbMock struct{}

func (PoetDbMock) GetProofMessage(proofRef []byte) ([]byte, error) { return proofRef, nil }

func (PoetDbMock) HasProof(proofRef []byte) bool { return true }

func (PoetDbMock) ValidateAndStore(proofMessage *types.PoetProofMessage) error { return nil }

func (*PoetDbMock) SubscribeToProofRef(poetId [types.PoetServiceIdLength]byte, roundId uint64) chan []byte {
	ch := make(chan []byte)
	go func() {
		ch <- []byte("hello there")
	}()
	return ch
}

func (*PoetDbMock) GetMembershipMap(poetRoot []byte) (map[types.Hash32]bool, error) {
	hash := types.BytesToHash([]byte("anton"))
	return map[types.Hash32]bool{hash: true}, nil
}

type BlockEligibilityValidatorMock struct {
}

func (BlockEligibilityValidatorMock) BlockSignedAndEligible(block *types.Block) (bool, error) {
	return true, nil
}

type SyntacticValidatorMock struct {
}

func (SyntacticValidatorMock) SyntacticallyValid(block *types.BlockHeader) (bool, error) {
	return true, nil
}

type MeshValidatorMock struct{}

func (m *MeshValidatorMock) GetGoodPatternBlocks(layer types.LayerID) (map[types.BlockID]struct{}, error) {
	panic("implement me")
}

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

func (MockState) ValidateSignature(signed types.Signed) (types.Address, error) {
	return types.Address{}, nil
}

func (MockState) ApplyRewards(layer types.LayerID, miners []types.Address, underQuota map[types.Address]int, bonusReward, diminishedReward *big.Int) {
}

func (MockState) ValidateTransactionSignature(tx *types.SerializableSignedTransaction) (types.Address, error) {
	return types.Address{}, nil
}

func (s *StateMock) ApplyRewards(layer types.LayerID, miners []string, underQuota map[string]int, bonusReward, diminishedReward *big.Int) {

}

type MockIStore struct {
}

func (*MockIStore) StoreNodeIdentity(id types.NodeId) error {
	return nil
}

func (*MockIStore) GetIdentity(id string) (types.NodeId, error) {
	return types.NodeId{Key: "some string ", VRFPublicKey: []byte("bytes")}, nil
}

type ValidatorMock struct{}

func (*ValidatorMock) Validate(nipst *types.NIPST, expectedChallenge types.Hash32) error {
	return nil
}

type MockTxMemPool struct{}

func (MockTxMemPool) Get(id types.TransactionId) (types.AddressableSignedTransaction, error) {
	return types.AddressableSignedTransaction{}, nil
}
func (MockTxMemPool) PopItems(size int) []types.AddressableSignedTransaction {
	return nil
}
func (MockTxMemPool) Put(id types.TransactionId, item *types.AddressableSignedTransaction) {

}
func (MockTxMemPool) Invalidate(id types.TransactionId) {

}

type MockAtxMemPool struct{}

func (MockAtxMemPool) Get(id types.AtxId) (types.ActivationTx, error) {
	return types.ActivationTx{}, nil
}

func (MockAtxMemPool) PopItems(size int) []types.ActivationTx {
	return nil
}

func (MockAtxMemPool) Put(id types.AtxId, item *types.ActivationTx) {

}

func (MockAtxMemPool) Invalidate(id types.AtxId) {

}
