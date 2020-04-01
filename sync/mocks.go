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

func (poetDbMock) HasProof([]byte) bool { return true }

func (poetDbMock) ValidateAndStore(*types.PoetProofMessage) error { return nil }

type BlockEligibilityValidatorMock struct {
}

func (BlockEligibilityValidatorMock) BlockSignedAndEligible(*types.Block) (bool, error) {
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

func (m *MeshValidatorMock) HandleLateBlock(bl *types.Block) (types.LayerID, types.LayerID) {
	return bl.Layer(), bl.Layer() - 1

}

type MockState struct{}

func (s MockState) LoadState(types.LayerID) error {
	panic("implement me")
}

func (s MockState) GetStateRoot() types.Hash32 {
	return [32]byte{}
}

func (MockState) ValidateNonceAndBalance(*types.Transaction) error {
	panic("implement me")
}

func (MockState) GetLayerApplied(types.TransactionID) *types.LayerID {
	panic("implement me")
}

func (MockState) ApplyTransactions(types.LayerID, []*types.Transaction) (int, error) {
	return 0, nil
}

func (MockState) ApplyRewards(types.LayerID, []types.Address, *big.Int) {
}

func (MockState) AddressExists(types.Address) bool {
	return true
}

type MockIStore struct{}

func (*MockIStore) StoreNodeIdentity(types.NodeID) error {
	return nil
}

func (*MockIStore) GetIdentity(string) (types.NodeID, error) {
	return types.NodeID{Key: "some string ", VRFPublicKey: []byte("bytes")}, nil
}

type ValidatorMock struct{}

func (*ValidatorMock) Validate(signing.PublicKey, *types.NIPST, types.Hash32) error {
	return nil
}

func (*ValidatorMock) VerifyPost(signing.PublicKey, *types.PostProof, uint64) error {
	return nil
}

type mockTxMemPool struct{}

func (mockTxMemPool) Get(types.TransactionID) (*types.Transaction, error) {
	return &types.Transaction{}, nil
}

func (mockTxMemPool) GetAllItems() []*types.Transaction {
	return nil
}

func (mockTxMemPool) Put(types.TransactionID, *types.Transaction) {}
func (mockTxMemPool) Invalidate(types.TransactionID)              {}

type mockAtxMemPool struct{}

func (mockAtxMemPool) Get(types.ATXID) (*types.ActivationTx, error) {
	return &types.ActivationTx{}, nil
}

func (mockAtxMemPool) GetAllItems() []types.ActivationTx {
	return nil
}

func (mockAtxMemPool) Put(*types.ActivationTx) {}
func (mockAtxMemPool) Invalidate(types.ATXID)  {}

type MockClock struct {
	ch         map[timesync.LayerTimer]int
	ids        map[int]timesync.LayerTimer
	countSub   int
	countUnsub int
	Interval   duration.Duration
	Layer      types.LayerID
}

func (m *MockClock) LayerToTime(types.LayerID) time.Time {
	return time.Now().Add(1000 * time.Hour) // hack so this wont take affect in the mock
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
