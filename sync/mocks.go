package sync

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/mesh"
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

func (MockState) ValidateSignature(signed types.Signed) (address.Address, error) {
	return address.Address{}, nil
}

func (MockState) ApplyRewards(layer types.LayerID, miners []address.Address, underQuota map[address.Address]int, bonusReward, diminishedReward *big.Int) {
}

func (MockState) ValidateTransactionSignature(tx types.SerializableSignedTransaction) (address.Address, error) {
	return address.Address{}, nil
}

func (s *StateMock) ApplyRewards(layer types.LayerID, miners []string, underQuota map[string]int, bonusReward, diminishedReward *big.Int) {

}

type AtxDbMock struct {
	db     map[types.AtxId]*types.ActivationTx
	nipsts map[types.AtxId]*types.NIPST
}

var _ mesh.AtxDB = &AtxDbMock{}

func (t *AtxDbMock) SyntacticallyValidateAtx(atx *types.ActivationTx) error {
	return nil
}

func NewAtxDbMock() *AtxDbMock {
	return &AtxDbMock{
		make(map[types.AtxId]*types.ActivationTx),
		make(map[types.AtxId]*types.NIPST),
	}
}

func (t *AtxDbMock) GetAtx(id types.AtxId) (*types.ActivationTx, error) {
	if atx, ok := t.db[id]; ok {
		return atx, nil
	}
	return nil, fmt.Errorf("cannot find atx")
}

func (t *AtxDbMock) ProcessAtx(atx *types.ActivationTx) {
	t.db[atx.Id()] = atx
	t.nipsts[atx.Id()] = atx.Nipst
}

//todo: if this is used somewhere then impl some real mock
func (t *AtxDbMock) GetEpochAtxIds(id types.EpochId) ([]types.AtxId, error) {
	return []types.AtxId{}, nil
}

func (t *AtxDbMock) GetNipst(id types.AtxId) (*types.NIPST, error) {
	return t.nipsts[id], nil
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

func (*ValidatorMock) Validate(nipst *types.NIPST, expectedChallenge common.Hash) error {
	return nil
}

type MemPoolMock struct {
}

func (mem *MemPoolMock) Get(id interface{}) interface{} {
	return nil
}

func (mem *MemPoolMock) PopItems(size int) interface{} {
	return nil
}

func (mem *MemPoolMock) Put(id interface{}, item interface{}) {
}

func (mem *MemPoolMock) Invalidate(id interface{}) {
}
