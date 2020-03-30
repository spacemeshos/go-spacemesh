package sync

import (
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/sha256-simd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

const atxLimit = 100

type PeersMock struct {
	getPeers func() []p2p.Peer
}

func (pm PeersMock) GetPeers() []p2p.Peer {
	return pm.getPeers()
}

func (pm PeersMock) Close() {
	return
}

func ListenerFactory(serv service.Service, peers peers, name string, layer types.LayerID) *BlockListener {
	sync := SyncFactory(name, serv)
	sync.peers = peers
	nbl := NewBlockListener(serv, sync, 2, log.New(name, "", ""))
	return nbl
}

func SyncFactory(name string, serv service.Service) *Syncer {
	tick := 20 * time.Second
	ts := timesync.NewClock(timesync.RealClock{}, tick, time.Now(), log.NewDefault("clock"))
	l := log.New(name, "", "")
	poetDb := activation.NewPoetDb(database.NewMemDatabase(), l.WithName("poetDb"))
	blockValidator := BlockEligibilityValidatorMock{}
	sync := NewSync(serv, getMesh(memoryDB, name), miner.NewTxMemPool(), miner.NewAtxMemPool(), blockValidator, poetDb, conf, ts, l)
	return sync
}

func TestBlockListener(t *testing.T) {
	sim := service.NewSimulator()
	signer := signing.NewEdSigner()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "listener1", 3)
	bl2 := ListenerFactory(n2, PeersMock{func() []p2p.Peer { return []p2p.Peer{n1.PublicKey()} }}, "listener2", 3)
	defer bl2.Close()
	defer bl1.Close()
	bl2.Start()
	atx1 := atx(signer.PublicKey().String())

	atx2 := atx(signer.PublicKey().String())
	atx3 := atx(signer.PublicKey().String())

	bl2.Start()

	proofMessage := makePoetProofMessage(t)
	if err := bl1.poetDb.ValidateAndStore(&proofMessage); err != nil {
		t.Error(err)
	}
	poetProofBytes, err := types.InterfaceToBytes(&proofMessage.PoetProof)
	if err != nil {
		t.Error(err)
	}
	poetRef := sha256.Sum256(poetProofBytes)

	atx1.Nipst.PostProof.Challenge = poetRef[:]
	atx2.Nipst.PostProof.Challenge = poetRef[:]
	atx3.Nipst.PostProof.Challenge = poetRef[:]

	err = activation.SignAtx(signer, atx1)
	assert.NoError(t, err)
	err = activation.SignAtx(signer, atx2)
	assert.NoError(t, err)
	err = activation.SignAtx(signer, atx3)
	assert.NoError(t, err)

	err = bl1.ProcessAtxs([]*types.ActivationTx{atx1, atx2, atx3})
	assert.NoError(t, err)

	err = bl2.ProcessAtxs([]*types.ActivationTx{atx1})
	assert.NoError(t, err)

	block1 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block1.Signature = signer.Sign(block1.Bytes())
	block1.ATXID = *types.EmptyATXID
	block2 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block2.Signature = signer.Sign(block2.Bytes())
	block2.ATXIDs = append(block2.ATXIDs, atx2.ID())
	block3 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block3.Signature = signer.Sign(block3.Bytes())
	block3.ATXIDs = append(block3.ATXIDs, atx3.ID())

	block2.Initialize()
	block3.Initialize()

	block1.AddView(block2.ID())
	block1.AddView(block3.ID())

	block1.Initialize()

	bl1.AddBlock(block1)
	bl1.AddBlock(block2)
	bl1.AddBlock(block3)

	_, err = bl1.GetBlock(block1.ID())
	if err != nil {
		t.Error(err)
	}

	bl2.syncLayer(0, []types.BlockID{block1.ID()})

	b, err := bl2.GetBlock(block1.ID())
	if err != nil {
		t.Error(err)
	}

	t.Log("  ", b)
	t.Log("done!")
}

// TODO: perform this test on a standalone syncer instead of a blockListener.
func TestBlockListener_DataAvailability(t *testing.T) {
	sim := service.NewSimulator()
	signer := signing.NewEdSigner()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "listener1", 3)
	bl2 := ListenerFactory(n2, PeersMock{func() []p2p.Peer { return []p2p.Peer{n1.PublicKey()} }}, "listener2", 3)
	defer bl2.Close()
	defer bl1.Close()
	bl2.Start()

	atx1 := atx(signer.PublicKey().String())

	// Push atx1 poet proof into bl1.

	proofMessage := makePoetProofMessage(t)
	err := bl1.poetDb.ValidateAndStore(&proofMessage)
	require.NoError(t, err)

	poetProofBytes, err := types.InterfaceToBytes(&proofMessage.PoetProof)
	require.NoError(t, err)
	poetRef := sha256.Sum256(poetProofBytes)

	atx1.Nipst.PostProof.Challenge = poetRef[:]
	err = activation.SignAtx(signer, atx1)
	assert.NoError(t, err)
	// Push a block with tx1 and and atx1 into bl1.

	block := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block.Signature = signer.Sign(block.Bytes())
	block.TxIDs = append(block.TxIDs, tx1.ID())
	block.ATXIDs = append(block.ATXIDs, atx1.ID())
	err = bl1.AddBlockWithTxs(block, []*types.Transaction{tx1}, []*types.ActivationTx{atx1})
	require.NoError(t, err)

	_, err = bl1.GetBlock(block.ID())
	require.NoError(t, err)

	// Verify that bl2 doesn't have them in mempool.

	_, err = bl2.txpool.Get(tx1.ID())
	require.EqualError(t, err, "transaction not found in mempool")
	_, err = bl2.atxpool.Get(atx1.ID())
	require.EqualError(t, err, "cannot find ATX in mempool")

	// Sync bl2.

	txs, atxs, err := bl2.dataAvailability(block)
	require.NoError(t, err)
	require.Equal(t, 1, len(txs))
	require.Equal(t, tx1.ID(), txs[0].ID())
	require.Equal(t, 1, len(atxs))
	require.Equal(t, atx1.ID(), atxs[0].ID())

	// Verify that bl2 inserted them to the mempool.

	_, err = bl2.txpool.Get(tx1.ID())
	require.NoError(t, err)
	_, err = bl2.atxpool.Get(atx1.ID())
	require.NoError(t, err)
}

func TestBlockListener_DataAvailabilityBadFlow(t *testing.T) {
	sim := service.NewSimulator()
	signer := signing.NewEdSigner()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "listener1", 3)
	bl2 := ListenerFactory(n2, PeersMock{func() []p2p.Peer { return []p2p.Peer{n1.PublicKey()} }}, "listener2", 3)
	defer bl2.Close()
	defer bl1.Close()
	bl2.Start()

	atx1 := atx(signer.PublicKey().String())

	// Push atx1 poet proof into bl1.

	proofMessage := makePoetProofMessage(t)
	err := bl1.poetDb.ValidateAndStore(&proofMessage)
	require.NoError(t, err)

	poetProofBytes, err := types.InterfaceToBytes(&proofMessage.PoetProof)
	require.NoError(t, err)
	poetRef := sha256.Sum256(poetProofBytes)

	atx1.Nipst.PostProof.Challenge = poetRef[:]

	// Push a block with tx1 and and atx1 into bl1.
	block := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block.Signature = signer.Sign(block.Bytes())
	block.TxIDs = append(block.TxIDs, tx1.ID())
	block.ATXIDs = append(block.ATXIDs, atx1.ID())

	// adding block to peer1
	err = bl1.AddBlockWithTxs(block, []*types.Transaction{}, []*types.ActivationTx{atx1})
	require.NoError(t, err)

	_, err = bl1.GetBlock(block.ID())
	require.NoError(t, err)

	_, _, err = bl2.dataAvailability(block)
	require.Error(t, err)

	// create a new ATX
	atx2 := atx(signer.PublicKey().String())

	poetProofBytes, err = types.InterfaceToBytes(&proofMessage.PoetProof)
	require.NoError(t, err)
	poetRef = sha256.Sum256(poetProofBytes)
	// attach proof to ATX
	atx2.Nipst.PostProof.Challenge = poetRef[:]
	// create a block containing tx2 and atx2
	tBlock := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	tBlock.Signature = signer.Sign(tBlock.Bytes())
	tBlock.TxIDs = append(tBlock.TxIDs, tx2.ID())
	tBlock.ATXIDs = append(tBlock.ATXIDs, atx2.ID())

	// Push tx2 poet proof into bl1.
	err = bl1.AddBlockWithTxs(tBlock, []*types.Transaction{tx2}, []*types.ActivationTx{})
	require.NoError(t, err)

	_, err = bl1.GetBlock(tBlock.ID())
	require.NoError(t, err)

	_, _, err = bl2.dataAvailability(tBlock)
	require.Error(t, err)
}

func TestBlockListener_ValidateVotesGoodFlow(t *testing.T) {
	block1 := types.NewExistingBlock(1, []byte(rand.RandString(8)))

	block2 := types.NewExistingBlock(1, []byte(rand.RandString(8)))

	block3 := types.NewExistingBlock(1, []byte(rand.RandString(8)))

	block4 := types.NewExistingBlock(1, []byte(rand.RandString(8)))

	block5 := types.NewExistingBlock(1, []byte(rand.RandString(8)))

	block6 := types.NewExistingBlock(1, []byte(rand.RandString(8)))

	block7 := types.NewExistingBlock(1, []byte(rand.RandString(8)))

	block1.AddView(block2.ID())
	block1.AddView(block3.ID())
	block1.AddView(block4.ID())
	block2.AddView(block5.ID())
	block2.AddView(block6.ID())
	block3.AddView(block6.ID())
	block4.AddView(block7.ID())
	block6.AddView(block7.ID())

	block1.AddVote(block2.ID())
	block1.AddVote(block3.ID())
	block2.AddVote(block5.ID())
	block3.AddVote(block6.ID())
	block4.AddVote(block7.ID())
	block6.AddVote(block7.ID())

	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "TestBlockListener_ValidateVotesGoodFlow", 2)
	defer bl1.Close()

	bl1.AddBlock(block1)
	bl1.AddBlock(block2)
	bl1.AddBlock(block3)
	bl1.AddBlock(block4)
	bl1.AddBlock(block5)
	bl1.AddBlock(block6)
	bl1.AddBlock(block7)
	valid, err := validateVotes(block1, bl1.ForBlockInView, bl1.Hdist, log.New("", "", ""))
	assert.NoError(t, err)
	assert.True(t, valid)
}

func TestBlockListener_ValidateVotesBadFlow(t *testing.T) {
	block1 := types.NewExistingBlock(7, []byte(rand.RandString(8)))

	block2 := types.NewExistingBlock(8, []byte(rand.RandString(8)))

	block3 := types.NewExistingBlock(8, []byte(rand.RandString(8)))

	block4 := types.NewExistingBlock(9, []byte(rand.RandString(8)))

	block5 := types.NewExistingBlock(9, []byte(rand.RandString(8)))

	block6 := types.NewExistingBlock(9, []byte(rand.RandString(8)))

	block7 := types.NewExistingBlock(10, []byte(rand.RandString(8)))

	block1.AddView(block2.ID())
	block1.AddView(block3.ID())
	//block1.AddView(4)
	block2.AddView(block5.ID())
	block2.AddView(block6.ID())
	block3.AddView(block6.ID())
	block4.AddView(block7.ID())
	block6.AddView(block7.ID())

	block1.AddVote(block2.ID())
	block1.AddVote(block3.ID())
	block1.AddVote(block4.ID())
	block2.AddVote(block5.ID())
	block3.AddVote(block6.ID())
	block4.AddVote(block7.ID())
	block6.AddVote(block7.ID())

	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "TestBlockListener_ValidateVotesBadFlow", 2)
	defer bl1.Close()
	bl1.AddBlock(block1)
	bl1.AddBlock(block2)
	bl1.AddBlock(block3)
	bl1.AddBlock(block4)
	bl1.AddBlock(block5)
	bl1.AddBlock(block6)
	bl1.AddBlock(block7)
	valid, err := validateVotes(block1, bl1.ForBlockInView, bl1.Hdist, log.New("", "", ""))
	assert.Error(t, err)
	assert.False(t, valid)
}

func TestBlockListenerViewTraversal(t *testing.T) {

	t.Log("TestBlockListener2 start")
	sim := service.NewSimulator()
	signer := signing.NewEdSigner()

	n1 := sim.NewNode()
	n2 := sim.NewNode()
	n3 := sim.NewNode()

	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "TestBlockListener_1", 2)
	bl2 := ListenerFactory(n2, PeersMock{func() []p2p.Peer { return []p2p.Peer{n1.PublicKey(), n3.PublicKey()} }}, "TestBlockListener_2", 2)
	bl3 := ListenerFactory(n3, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "TestBlockListener_2", 2)
	defer bl2.Close()
	defer bl1.Close()
	bl2.Start()

	atx := atx(signer.PublicKey().String())

	byts, _ := types.InterfaceToBytes(atx)
	var atx1 types.ActivationTx
	types.BytesToInterface(byts, &atx1)
	atx1.CalcAndSetID()

	err := bl1.ProcessAtxs([]*types.ActivationTx{atx})
	assert.NoError(t, err)
	err = bl2.ProcessAtxs([]*types.ActivationTx{&atx1})
	assert.NoError(t, err)

	block1 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block1.ATXID = *types.EmptyATXID
	block1.Signature = signer.Sign(block1.Bytes())

	block2 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block2.ATXID = *types.EmptyATXID
	block2.Signature = signer.Sign(block2.Bytes())

	block3 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block3.ATXID = *types.EmptyATXID
	block3.Signature = signer.Sign(block3.Bytes())

	block4 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block4.ATXID = *types.EmptyATXID
	block4.Signature = signer.Sign(block4.Bytes())

	block5 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block5.ATXID = *types.EmptyATXID
	block5.Signature = signer.Sign(block5.Bytes())

	block6 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block6.ATXID = *types.EmptyATXID
	block6.Signature = signer.Sign(block6.Bytes())

	block7 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block7.ATXID = *types.EmptyATXID
	block7.Signature = signer.Sign(block7.Bytes())

	block8 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block8.ATXID = *types.EmptyATXID
	block8.Signature = signer.Sign(block8.Bytes())

	block9 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block9.ATXID = *types.EmptyATXID
	block9.Signature = signer.Sign(block9.Bytes())

	block10 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block10.ATXID = *types.EmptyATXID
	block10.Signature = signer.Sign(block10.Bytes())

	block11 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block11.ATXID = *types.EmptyATXID
	block11.Signature = signer.Sign(block11.Bytes())

	block1.Initialize()
	block2.AddView(block1.ID())
	block2.Initialize()
	block3.AddView(block2.ID())
	block3.Initialize()
	block4.AddView(block2.ID())
	block4.Initialize()
	block5.AddView(block3.ID())
	block5.AddView(block4.ID())
	block5.Initialize()
	block6.AddView(block4.ID())
	block6.Initialize()
	block7.AddView(block6.ID())
	block7.AddView(block5.ID())
	block7.Initialize()
	block8.AddView(block7.ID())
	block8.Initialize()
	block9.AddView(block5.ID())
	block9.Initialize()
	block10.AddView(block8.ID())
	block10.AddView(block9.ID())
	block10.Initialize()

	bl1.AddBlock(block1)
	bl1.AddBlock(block2)
	bl1.AddBlock(block3)
	bl1.AddBlock(block4)
	bl1.AddBlock(block5)
	bl1.AddBlock(block6)
	bl1.AddBlock(block7)
	bl1.AddBlock(block8)
	bl1.AddBlock(block9)
	bl1.AddBlock(block10)
	bl3.AddBlock(block11)

	bl2.syncLayer(1, []types.BlockID{block10.ID(), block11.ID()})

	b, err := bl2.GetBlock(block10.ID())
	if err != nil {
		t.Error(err)
	}

	b, err = bl2.GetBlock(block11.ID())
	if err != nil {
		t.Error(err)
	}

	b, err = bl1.GetBlock(block1.ID())
	if err != nil {
		t.Error(err)
	}

	b, err = bl2.GetBlock(block1.ID())
	if err != nil {
		t.Error(err)
	}

	_, err = bl2.GetBlock(block2.ID())
	if err != nil {
		t.Error(err)
	}

	_, err = bl2.GetBlock(block3.ID())
	if err != nil {
		t.Error(err)
	}

	_, err = bl2.GetBlock(block4.ID())
	if err != nil {
		t.Error(err)
	}

	b, err = bl2.GetBlock(block5.ID())
	if err != nil {
		t.Error(err)
	}

	assert.Equal(t, 0, len(bl2.blockQueue.pending))

	t.Log("  ", b)
	t.Log("done!")
}

func TestBlockListener_TraverseViewBadFlow(t *testing.T) {

	t.Log("TestBlockListener2 start")
	sim := service.NewSimulator()
	signer := signing.NewEdSigner()

	n1 := sim.NewNode()
	n2 := sim.NewNode()

	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "TestBlockListener_1", 2)
	bl2 := ListenerFactory(n2, PeersMock{func() []p2p.Peer { return []p2p.Peer{n1.PublicKey()} }}, "TestBlockListener_2", 2)
	defer bl2.Close()
	defer bl1.Close()
	bl2.Start()

	atx := atx(signer.PublicKey().String())

	byts, _ := types.InterfaceToBytes(atx)
	var atx1 types.ActivationTx
	types.BytesToInterface(byts, &atx1)
	atx1.CalcAndSetID()

	err := bl1.ProcessAtxs([]*types.ActivationTx{atx})
	assert.NoError(t, err)
	err = bl2.ProcessAtxs([]*types.ActivationTx{&atx1})
	assert.NoError(t, err)

	block1 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block1.ATXID = *types.EmptyATXID
	block1.Signature = signer.Sign(block1.Bytes())

	block2 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block2.ATXID = *types.EmptyATXID
	block2.Signature = signer.Sign(block2.Bytes())

	block3 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block3.ATXID = *types.EmptyATXID
	block3.Signature = signer.Sign(block3.Bytes())

	block4 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block4.ATXID = *types.EmptyATXID
	block4.Signature = signer.Sign(block4.Bytes())

	block5 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block5.ATXID = *types.EmptyATXID
	block5.Signature = signer.Sign(block5.Bytes())

	block6 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block6.ATXID = *types.EmptyATXID
	block6.Signature = signer.Sign(block5.Bytes())

	block2.AddView(block1.ID())
	block3.AddView(block2.ID())
	block4.AddView(block2.ID())
	block5.AddView(block3.ID())
	block5.AddView(block4.ID())

	bl1.AddBlock(block2)
	bl1.AddBlock(block3)
	bl1.AddBlock(block4)
	bl1.AddBlock(block5)

	go bl2.syncLayer(5, []types.BlockID{block5.ID(), block6.ID()})
	time.Sleep(1 * time.Second) //wait for fetch

	b, err := bl2.GetBlock(block1.ID())
	assert.Error(t, err)

	_, err = bl2.GetBlock(block2.ID())
	assert.Error(t, err)

	_, err = bl2.GetBlock(block3.ID())
	assert.Error(t, err)

	_, err = bl2.GetBlock(block4.ID())
	assert.Error(t, err)

	b, err = bl2.GetBlock(block5.ID())
	assert.Error(t, err)

	assert.Equal(t, 0, len(bl2.blockQueue.pending))

	t.Log("  ", b)
	t.Log("done!")
}

func TestBlockListener_ListenToGossipBlocks(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "TestBlockListener_ListenToGossipBlocks1", 1)
	bl2 := ListenerFactory(n2, PeersMock{func() []p2p.Peer { return []p2p.Peer{n1.PublicKey()} }}, "TestBlockListener_ListenToGossipBlocks2", 1)

	bl1.Start()
	bl1.Syncer.Start()
	bl2.Start()

	tx, err := mesh.NewSignedTx(1, types.BytesToAddress([]byte{0x01}), 10, 100, 10, signing.NewEdSigner())
	assert.NoError(t, err)
	signer := signing.NewEdSigner()
	atx := atx(signer.PublicKey().String())

	proofMessage := makePoetProofMessage(t)
	if err := bl1.poetDb.ValidateAndStore(&proofMessage); err != nil {
		t.Error(err)
	}

	poetProofBytes, err := types.InterfaceToBytes(&proofMessage.PoetProof)
	if err != nil {
		t.Error(err)
	}

	poetRef := sha256.Sum256(poetProofBytes)
	atx.Nipst.PostProof.Challenge = poetRef[:]
	err = activation.SignAtx(signer, atx)
	assert.NoError(t, err)

	blk := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	blk.TxIDs = append(blk.TxIDs, tx.ID())
	blk.ATXIDs = append(blk.ATXIDs, atx.ID())
	blk.Signature = signer.Sign(blk.Bytes())
	blk.Initialize()

	bl2.AddBlockWithTxs(blk, []*types.Transaction{tx}, []*types.ActivationTx{atx})

	data, err := types.InterfaceToBytes(&blk)
	require.NoError(t, err)
	bl1.ForceSync()
	err = n2.Broadcast(config.NewBlockProtocol, data)
	assert.NoError(t, err)

	time.Sleep(1 * time.Second)
	timeout := time.After(2 * time.Second)
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
			return
		default:
			if b, err := bl1.GetBlock(blk.ID()); err == nil {
				res, err := blk.Compare(b)
				assert.NoError(t, err)
				assert.True(t, res)
				t.Log("done!")
				return
			}
		}
	}
}

func TestBlockListener_AtxCache(t *testing.T) {
	sim := service.NewSimulator()
	signer := signing.NewEdSigner()
	n1 := sim.NewNode()
	//n2 := sim.NewNode()
	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{ /*n2.PublicKey()*/ } }}, "listener1", 3)

	atxDb := mesh.NewAtxDbMock()
	bl1.Mesh.AtxDB = atxDb

	defer bl1.Close()

	atx1 := atx(signer.PublicKey().String())
	atx2 := atx(signer.PublicKey().String())

	// Push block with tx1 and and atx1 into bl1.
	blk1 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	blk1.Signature = signer.Sign(blk1.Bytes())
	blk1.TxIDs = append(blk1.TxIDs, tx1.ID())
	blk1.ATXIDs = append(blk1.ATXIDs, atx1.ID())
	blk1.ATXIDs = append(blk1.ATXIDs, atx2.ID())

	err := bl1.AddBlockWithTxs(blk1, []*types.Transaction{tx1}, []*types.ActivationTx{atx1, atx2})
	require.NoError(t, err)
	require.Equal(t, 2, atxDb.ProcCnt)
	_, err = bl1.GetBlock(blk1.ID())
	require.NoError(t, err)

	// Push different block with same transactions - not expected to process atxs
	blk2 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	blk2.Signature = signer.Sign(blk2.Bytes())
	blk2.TxIDs = append(blk2.TxIDs, tx1.ID())
	blk2.ATXIDs = append(blk2.ATXIDs, atx1.ID())
	blk2.ATXIDs = append(blk2.ATXIDs, atx2.ID())

	err = bl1.AddBlockWithTxs(blk2, []*types.Transaction{tx1}, []*types.ActivationTx{atx1, atx2})
	require.NoError(t, err)
	require.Equal(t, 4, atxDb.ProcCnt)
	_, err = bl1.GetBlock(blk2.ID())
	require.NoError(t, err)

	// Push different block with subset of transactions - expected to process atxs
	blk3 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	blk3.Signature = signer.Sign(blk3.Bytes())
	blk3.TxIDs = append(blk3.TxIDs, tx1.ID())
	blk3.ATXIDs = append(blk3.ATXIDs, atx1.ID())

	err = bl1.AddBlockWithTxs(blk3, []*types.Transaction{tx1}, []*types.ActivationTx{atx1})
	require.NoError(t, err)
	require.Equal(t, 5, atxDb.ProcCnt)
	_, err = bl1.GetBlock(blk3.ID())
	require.NoError(t, err)
}

//todo integration testing
