package blocks

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/rand"
)

func init() {
	types.SetLayersPerEpoch(3)
}

var commitment = &types.PostProof{
	Challenge:    []byte(nil),
	MerkleRoot:   []byte("1"),
	ProofNodes:   [][]byte(nil),
	ProvenLeaves: [][]byte(nil),
}

var goldenATXID = types.ATXID(types.HexToHash32("77777"))

func newActivationTx(nodeID types.NodeID, sequence uint64, prevATX types.ATXID, pubLayerID types.LayerID,
	startTick uint64, positioningATX types.ATXID, coinbase types.Address, nipst *types.NIPST) *types.ActivationTx {

	nipstChallenge := types.NIPSTChallenge{
		NodeID:         nodeID,
		Sequence:       sequence,
		PrevATXID:      prevATX,
		PubLayerID:     pubLayerID,
		StartTick:      startTick,
		PositioningATX: positioningATX,
	}
	return types.NewActivationTx(nipstChallenge, coinbase, nipst, 1024, nil)
}

func atx(pubkey string) *types.ActivationTx {
	coinbase := types.HexToAddress("aaaa")
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0xde, 0xad}
	npst := activation.NewNIPSTWithChallenge(&chlng, poetRef)

	atx := newActivationTx(types.NodeID{Key: pubkey, VRFPublicKey: []byte(rand.String(8))}, 0, *types.EmptyATXID, 5, 1, goldenATXID, coinbase, npst)
	atx.Commitment = commitment
	atx.CommitmentMerkleRoot = commitment.MerkleRoot
	atx.CalcAndSetID()
	return atx
}

func genByte32() [32]byte {
	var x [32]byte
	rand.Read(x[:])
	return x
}

var txid1 = types.TransactionID(genByte32())
var txid2 = types.TransactionID(genByte32())
var txid3 = types.TransactionID(genByte32())

var one = types.CalcHash32([]byte("1"))
var two = types.CalcHash32([]byte("2"))
var three = types.CalcHash32([]byte("3"))

var atx1 = types.ATXID(one)
var atx2 = types.ATXID(two)
var atx3 = types.ATXID(three)

type fetchMock struct {
	retError       bool
	getBlockCalled map[types.BlockID]int
	getAtxCalled   map[types.ATXID]int
	getTxsCalled   map[types.TransactionID]int
}

func (f fetchMock) FetchBlock(ctx context.Context, ID types.BlockID) error {
	f.getBlockCalled[ID]++
	return f.returnError()
}

func (f fetchMock) ListenToGossip() bool {
	return true
}

func (f fetchMock) IsSynced(context.Context) bool {
	return true
}

func newFetchMock() *fetchMock {
	return &fetchMock{
		retError:       false,
		getBlockCalled: make(map[types.BlockID]int),
		getAtxCalled:   make(map[types.ATXID]int),
		getTxsCalled:   make(map[types.TransactionID]int),
	}
}

func (f fetchMock) returnError() error {
	if f.retError {
		return fmt.Errorf("error")
	}
	return nil
}

func (f *fetchMock) GetBlock(ID types.BlockID) error {
	f.getBlockCalled[ID]++
	return f.returnError()
}

func (f fetchMock) FetchAtx(ctx context.Context, ID types.ATXID) error {
	return f.returnError()
}

func (f fetchMock) GetPoetProof(ctx context.Context, ID types.Hash32) error {
	return f.returnError()
}

func (f fetchMock) GetTxs(ctx context.Context, IDs []types.TransactionID) error {
	return f.returnError()
}

func (f fetchMock) GetBlocks(ctx context.Context, IDs []types.BlockID) error {
	return f.returnError()
}

func (f fetchMock) GetAtxs(ctx context.Context, IDs []types.ATXID) error {
	return f.returnError()
}

type meshMock struct {
}

func (m meshMock) ForBlockInView(view map[types.BlockID]struct{}, layer types.LayerID, blockHandler func(block *types.Block) (bool, error)) error {
	panic("implement me")
}

func (m meshMock) GetBlock(ID types.BlockID) (*types.Block, error) {
	panic("implement me")
}

func (m meshMock) AddBlockWithTxs(blk *types.Block) error {
	panic("implement me")
}

func (m meshMock) ProcessedLayer() types.LayerID {
	panic("implement me")
}

func (m meshMock) HandleLateBlock(blk *types.Block) {
	panic("implement me")
}

type verifierMock struct {
}

func (v verifierMock) BlockSignedAndEligible(block *types.Block) (bool, error) {
	return true, nil
}

func Test_validateUniqueTxAtx(t *testing.T) {
	r := require.New(t)
	b := &types.Block{}

	// unique
	b.TxIDs = []types.TransactionID{txid1, txid2, txid3}
	b.ActiveSet = &[]types.ATXID{atx1, atx2, atx3}
	r.Nil(validateUniqueTxAtx(b))

	// dup txs
	b.TxIDs = []types.TransactionID{txid1, txid2, txid1}
	b.ActiveSet = &[]types.ATXID{atx1, atx2, atx3}
	r.EqualError(validateUniqueTxAtx(b), errDupTx.Error())

	// dup atxs
	b.TxIDs = []types.TransactionID{txid1, txid2, txid3}
	b.ActiveSet = &[]types.ATXID{atx1, atx2, atx1}
	r.EqualError(validateUniqueTxAtx(b), errDupAtx.Error())
}

func TestBlockHandler_BlockSyntacticValidation(t *testing.T) {
	r := require.New(t)
	cfg := Config{3, goldenATXID}
	//yncs, _, _ := SyncMockFactory(2, conf, "TestSyncProtocol_NilResponse", memoryDB, newMemPoetDb)
	s := NewBlockHandler(cfg, &meshMock{}, &verifierMock{}, log.NewDefault("BlockSyntacticValidation"))
	b := &types.Block{}

	fetch := newFetchMock()
	err := s.blockSyntacticValidation(context.TODO(), b, fetch)
	r.EqualError(err, errNoActiveSet.Error())

	b.ActiveSet = &[]types.ATXID{}
	err = s.blockSyntacticValidation(context.TODO(), b, fetch)
	r.EqualError(err, errZeroActiveSet.Error())

	b.ActiveSet = &[]types.ATXID{atx1, atx2, atx3}
	b.TxIDs = []types.TransactionID{txid1, txid2, txid1}
	err = s.blockSyntacticValidation(context.TODO(), b, fetch)
	r.EqualError(err, errDupTx.Error())
}

func mockForBlockInView(view map[types.BlockID]struct{}, layer types.LayerID, blockHandler func(block *types.Block) (bool, error)) error {
	return nil
}

func TestBlockHandler_BlockSyntacticValidation_syncRefBlock(t *testing.T) {
	r := require.New(t)
	fetch := newFetchMock()
	atxpool := activation.NewAtxMemPool()
	cfg := Config{
		3, goldenATXID,
	}
	s := NewBlockHandler(cfg, &meshMock{}, &verifierMock{}, log.NewDefault("syncRefBlock"))
	s.traverse = mockForBlockInView
	a := atx("")
	atxpool.Put(a)
	b := &types.Block{}
	b.TxIDs = []types.TransactionID{}
	block1 := types.NewExistingBlock(1, []byte(rand.String(8)), nil)
	block1.ActiveSet = &[]types.ATXID{a.ID()}
	block1.ATXID = a.ID()
	block1.Initialize()
	block1ID := block1.ID()
	b.RefBlock = &block1ID
	b.ATXID = a.ID()
	fetch.retError = true
	err := s.blockSyntacticValidation(context.TODO(), b, fetch)
	r.Equal(err, fmt.Errorf("failed to fetch ref block %v e: error", *b.RefBlock))

	fetch.retError = false
	err = s.blockSyntacticValidation(context.TODO(), b, fetch)
	r.NoError(err)
	assert.Equal(t, 2, fetch.getBlockCalled[block1ID])
}

func TestBlockHandler_AtxSetID(t *testing.T) {
	a := atx("")
	bbytes, _ := types.InterfaceToBytes(*a)
	var b types.ActivationTx
	types.BytesToInterface(bbytes, &b)
	t.Log(fmt.Sprintf("%+v\n", *a))
	t.Log("---------------------")
	t.Log(fmt.Sprintf("%+v\n", b))
	t.Log("---------------------")
	assert.Equal(t, b.Nipst, a.Nipst)
	assert.Equal(t, b.Commitment, a.Commitment)

	assert.Equal(t, b.ActivationTxHeader.NodeID, a.ActivationTxHeader.NodeID)
	assert.Equal(t, b.ActivationTxHeader.PrevATXID, a.ActivationTxHeader.PrevATXID)
	assert.Equal(t, b.ActivationTxHeader.Coinbase, a.ActivationTxHeader.Coinbase)
	assert.Equal(t, b.ActivationTxHeader.CommitmentMerkleRoot, a.ActivationTxHeader.CommitmentMerkleRoot)
	assert.Equal(t, b.ActivationTxHeader.NIPSTChallenge, a.ActivationTxHeader.NIPSTChallenge)
	b.CalcAndSetID()
	assert.Equal(t, a.ShortString(), b.ShortString())
}
