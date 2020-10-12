package blocks

import (
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/require"
	"testing"
)

var commitment = &types.PostProof{
	Challenge:    []byte(nil),
	MerkleRoot:   []byte("1"),
	ProofNodes:   [][]byte(nil),
	ProvenLeaves: [][]byte(nil),
}

func newActivationTx(nodeID types.NodeID, sequence uint64, prevATX types.ATXID, pubLayerID types.LayerID,
	startTick uint64, positioningATX types.ATXID, coinbase types.Address, activeSetSize uint32, view []types.BlockID,
	nipst *types.NIPST) *types.ActivationTx {

	nipstChallenge := types.NIPSTChallenge{
		NodeID:         nodeID,
		Sequence:       sequence,
		PrevATXID:      prevATX,
		PubLayerID:     pubLayerID,
		StartTick:      startTick,
		PositioningATX: positioningATX,
	}
	return types.NewActivationTx(nipstChallenge, coinbase, nipst, nil)
}

func atx(pubkey string) *types.ActivationTx {
	coinbase := types.HexToAddress("aaaa")
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0xde, 0xad}
	npst := activation.NewNIPSTWithChallenge(&chlng, poetRef)

	atx := newActivationTx(types.NodeID{Key: pubkey, VRFPublicKey: []byte(rand.String(8))}, 0, *types.EmptyATXID, 5, 1, *types.EmptyATXID, coinbase, 0, nil, npst)
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

func TestSyncer_BlockSyntacticValidation(t *testing.T) {
	r := require.New(t)
	//yncs, _, _ := SyncMockFactory(2, conf, "TestSyncProtocol_NilResponse", memoryDB, newMemPoetDb)
	s := syncs[0]
	s.atxDb = alwaysOkAtxDb{}
	b := &types.Block{}
	b.TxIDs = []types.TransactionID{txid1, txid2, txid1}
	_, _, err := s.blockSyntacticValidation(b)
	r.EqualError(err, errNoActiveSet.Error())

	b.ActiveSet = &[]types.ATXID{}
	_, _, err = s.blockSyntacticValidation(b)
	r.EqualError(err, errZeroActiveSet.Error())

	b.ActiveSet = &[]types.ATXID{atx1, atx2, atx3}
	_, _, err = s.blockSyntacticValidation(b)
	r.EqualError(err, errDupTx.Error())
}

func TestSyncer_BlockSyntacticValidation_syncRefBlock(t *testing.T) {
	r := require.New(t)
	syncs, _, _ := SyncMockFactory(2, conf, "BlockSyntacticValidation_syncRefBlock", memoryDB, newMemPoetDb)
	atxpool := activation.NewAtxMemPool()
	s := syncs[0]
	s.atxDb = atxpool
	a := atx("")
	atxpool.Put(a)
	b := &types.Block{}
	b.TxIDs = []types.TransactionID{}
	block1 := types.NewExistingBlock(1, []byte(rand.String(8)))
	block1.ActiveSet = &[]types.ATXID{a.ID()}
	block1.ATXID = a.ID()
	block1.Initialize()
	block1ID := block1.ID()
	b.RefBlock = &block1ID
	b.ATXID = a.ID()
	_, _, err := s.blockSyntacticValidation(b)
	r.Equal(err, fmt.Errorf("failed to fetch ref block %v", *b.RefBlock))

	err = syncs[1].AddBlock(block1)
	r.NoError(err)
	_, _, err = s.blockSyntacticValidation(b)
	r.NoError(err)
}

func TestSyncer_fetchBlock(t *testing.T) {
	r := require.New(t)
	atxPool := activation.NewAtxMemPool()
	syncs, _, _ := SyncMockFactory(2, conf, "fetchBlock", memoryDB, newMemPoetDb)
	s := syncs[0]
	s.atxDb = atxPool
	atx := atx("")
	atxPool.Put(atx)
	block1 := types.NewExistingBlock(1, []byte(rand.String(8)))
	block1.ActiveSet = &[]types.ATXID{atx.ID()}
	block1.ATXID = atx.ID()
	block1.Initialize()
	block1ID := block1.ID()
	res := s.fetchBlock(block1ID)
	r.False(res)
	err := syncs[1].AddBlock(block1)
	r.NoError(err)
	res = s.fetchBlock(block1ID)
	r.True(res)

}

func TestSyncer_AtxSetID(t *testing.T) {
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
