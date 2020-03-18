package sync

import (
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"math/big"
	"time"
)

type poetDbMock struct{}

func (poetDbMock) GetProofMessage(proofRef []byte) ([]byte, error) { return proofRef, nil }

func (poetDbMock) HasProof(proofRef []byte) bool { return true }

func (poetDbMock) ValidateAndStore(proofMessage *types.PoetProofMessage) error { return nil }

func (*poetDbMock) SubscribeToProofRef(poetID []byte, roundID string) chan []byte {
	ch := make(chan []byte)
	go func() {
		ch <- []byte("hello there")
	}()
	return ch
}

func (*poetDbMock) GetMembershipMap(poetRoot []byte) (map[types.Hash32]bool, error) {
	hash := types.BytesToHash([]byte("anton"))
	return map[types.Hash32]bool{hash: true}, nil
}

type BlockEligibilityValidatorMock struct {
}

func (BlockEligibilityValidatorMock) BlockSignedAndEligible(block *types.Block) (bool, error) {
	return true, nil
}

type syntacticValidatorMock struct {
}

func (syntacticValidatorMock) SyntacticallyValid(block *types.BlockHeader) (bool, error) {
	return true, nil
}

type MeshValidatorMock struct {
	delay           time.Duration
	calls           int
	layers          *mesh.Mesh
	vl              types.LayerID // the validated layer
	countValidated  int
	countValidate   int
	validatedLayers map[types.LayerID]struct{}
}

func (m *MeshValidatorMock) LatestComplete() types.LayerID {
	panic("implement me")
}

func (m *MeshValidatorMock) Persist() error {
	return nil
}

func (m *MeshValidatorMock) HandleIncomingLayer(lyr *types.Layer) (types.LayerID, types.LayerID) {
	m.countValidate++
	m.calls++
	m.vl = lyr.Index()
	if m.validatedLayers == nil {
		m.validatedLayers = make(map[types.LayerID]struct{})
	}
	m.validatedLayers[lyr.Index()] = struct{}{}
	time.Sleep(m.delay)
	return lyr.Index(), lyr.Index() - 1
}

func (m *MeshValidatorMock) GetGoodPatternBlocks(layer types.LayerID) (map[types.BlockID]struct{}, error) {
	panic("implement me")
}

func (m *MeshValidatorMock) HandleLateBlock(bl *types.Block) (types.LayerID, types.LayerID) {
	return bl.Layer(), bl.Layer() - 1

}

func (m *MeshValidatorMock) RegisterLayerCallback(func(id types.LayerID)) {}
func (m *MeshValidatorMock) ContextualValidity(id types.BlockID) bool     { return true }

type MockState struct{}

func (s MockState) LoadState(layer types.LayerID) error {
	panic("implement me")
}

func (s MockState) GetStateRoot() types.Hash32 {
	return [32]byte{}
}

func (MockState) ValidateNonceAndBalance(transaction *types.Transaction) error {
	panic("implement me")
}

func (MockState) GetLayerApplied(txID types.TransactionId) *types.LayerID {
	panic("implement me")
}

func (MockState) ApplyTransactions(layer types.LayerID, txs []*types.Transaction) (int, error) {
	return 0, nil
}

func (MockState) ValidateSignature(signed types.Signed) (types.Address, error) {
	return types.Address{}, nil
}

func (MockState) ApplyRewards(layer types.LayerID, miners []types.Address, reward *big.Int) {
}

func (MockState) AddressExists(addr types.Address) bool {
	return true
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

func (*ValidatorMock) Validate(id signing.PublicKey, nipst *types.NIPST, expectedChallenge types.Hash32) error {
	return nil
}

func (*ValidatorMock) VerifyPost(id signing.PublicKey, proof *types.PostProof, space uint64) error {
	return nil
}

type mockTxMemPool struct{}

func (mockTxMemPool) Get(id types.TransactionId) (*types.Transaction, error) {
	return &types.Transaction{}, nil
}
func (mockTxMemPool) GetAllItems() []*types.Transaction {
	return nil
}
func (mockTxMemPool) Put(id types.TransactionId, item *types.Transaction) {

}
func (mockTxMemPool) Invalidate(id types.TransactionId) {

}

type mockAtxMemPool struct{}

func (mockAtxMemPool) Get(id types.AtxId) (*types.ActivationTx, error) {
	return &types.ActivationTx{}, nil
}

func (mockAtxMemPool) GetAllItems() []types.ActivationTx {
	return nil
}

func (mockAtxMemPool) Put(atx *types.ActivationTx) {

}

func (mockAtxMemPool) Invalidate(id types.AtxId) {

}

type MockClock struct {
	ch         map[timesync.LayerTimer]int
	ids        map[int]timesync.LayerTimer
	countSub   int
	countUnsub int
	Interval   duration.Duration
	Layer      types.LayerID
}

func (m *MockClock) LayerToTime(types.LayerID) time.Time {
	return time.Now().Add(1000 * time.Hour) //hack so this wont take affect in the mock
}

func (m *MockClock) Tick() {
	l := m.GetCurrentLayer()
	log.Info("tick %v", l)
	for _, c := range m.ids {
		c <- l
	}
}

func (m *MockClock) GetCurrentLayer() types.LayerID {
	return m.Layer
}

func (m *MockClock) Subscribe() timesync.LayerTimer {
	m.countSub++

	if m.ch == nil {
		m.ch = make(map[timesync.LayerTimer]int)
		m.ids = make(map[int]timesync.LayerTimer)
	}
	newCh := make(chan types.LayerID, 1)
	m.ch[newCh] = len(m.ch)
	m.ids[len(m.ch)] = newCh

	return newCh
}

func (m *MockClock) Unsubscribe(timer timesync.LayerTimer) {
	m.countUnsub++
	delete(m.ids, m.ch[timer])
	delete(m.ch, timer)
}
