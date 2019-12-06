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

type PoetDbMock struct{}

func (PoetDbMock) GetProofMessage(proofRef []byte) ([]byte, error) { return proofRef, nil }

func (PoetDbMock) HasProof(proofRef []byte) bool { return true }

func (PoetDbMock) ValidateAndStore(proofMessage *types.PoetProofMessage) error { return nil }

func (*PoetDbMock) SubscribeToProofRef(poetId []byte, roundId string) chan []byte {
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

type MeshValidatorMock struct {
	delay           time.Duration
	calls           int
	layers          *mesh.Mesh
	vl              types.LayerID // the validated layer
	countValidated  int
	countValidate   int
	validatedLayers map[types.LayerID]struct{}
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

func (m *MeshValidatorMock) HandleLateBlock(bl *types.Block)              {}
func (m *MeshValidatorMock) RegisterLayerCallback(func(id types.LayerID)) {}
func (mlg *MeshValidatorMock) ContextualValidity(id types.BlockID) bool   { return true }

type StateMock struct{}

func (s *StateMock) ApplyTransactions(id types.LayerID, tx []*types.Transaction) (uint32, error) {
	return 0, nil
}

func ConfigTst() mesh.Config {
	return mesh.Config{
		BaseReward: big.NewInt(5000),
	}
}

type MockState struct{}

func (MockState) ValidateNonceAndBalance(transaction *types.Transaction) error {
	panic("implement me")
}

func (MockState) GetLayerApplied(txId types.TransactionId) *types.LayerID {
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

func (*ValidatorMock) Validate(id signing.PublicKey, nipst *types.NIPST, expectedChallenge types.Hash32) error {
	return nil
}

func (*ValidatorMock) VerifyPost(id signing.PublicKey, proof *types.PostProof, space uint64) error {
	return nil
}

type MockTxMemPool struct{}

func (MockTxMemPool) Get(id types.TransactionId) (*types.Transaction, error) {
	return &types.Transaction{}, nil
}
func (MockTxMemPool) GetAllItems() []*types.Transaction {
	return nil
}
func (MockTxMemPool) Put(id types.TransactionId, item *types.Transaction) {

}
func (MockTxMemPool) Invalidate(id types.TransactionId) {

}

type MockAtxMemPool struct{}

func (MockAtxMemPool) Get(id types.AtxId) (*types.ActivationTx, error) {
	return &types.ActivationTx{}, nil
}

func (MockAtxMemPool) GetAllItems() []types.ActivationTx {
	return nil
}

func (MockAtxMemPool) Put(atx *types.ActivationTx) {

}

func (MockAtxMemPool) Invalidate(id types.AtxId) {

}

type MockClock struct {
	ch         map[timesync.LayerTimer]int
	ids        map[int]timesync.LayerTimer
	countSub   int
	countUnsub int
	Interval   duration.Duration
	Layer      types.LayerID
}

func (c *MockClock) Tick() {
	l := c.GetCurrentLayer()
	log.Info("tick %v", l)
	for _, c := range c.ids {
		c <- l
	}
}

func (c *MockClock) GetCurrentLayer() types.LayerID {
	return c.Layer
}

func (c *MockClock) Subscribe() timesync.LayerTimer {
	c.countSub++

	if c.ch == nil {
		c.ch = make(map[timesync.LayerTimer]int)
		c.ids = make(map[int]timesync.LayerTimer)
	}
	newCh := make(chan types.LayerID, 1)
	c.ch[newCh] = len(c.ch)
	c.ids[len(c.ch)] = newCh

	return newCh
}

func (c *MockClock) Unsubscribe(timer timesync.LayerTimer) {
	c.countUnsub++
	delete(c.ids, c.ch[timer])
	delete(c.ch, timer)
}
