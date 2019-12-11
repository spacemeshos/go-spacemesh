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

func ListenerFactory(serv service.Service, peers p2p.Peers, name string, layer types.LayerID) *BlockListener {
	sync := SyncFactory(name, serv)
	sync.Peers = peers
	nbl := NewBlockListener(serv, sync, 2, log.New(name, "", ""))
	return nbl
}

func SyncFactory(name string, serv service.Service) *Syncer {
	tick := 20 * time.Second
	ts := timesync.NewTicker(timesync.RealClock{}, tick, time.Now())
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

	_, err = activation.SignAtx(signer, atx1)
	assert.NoError(t, err)
	_, err = activation.SignAtx(signer, atx2)
	assert.NoError(t, err)
	_, err = activation.SignAtx(signer, atx3)
	assert.NoError(t, err)

	err = bl1.ProcessAtxs([]*types.ActivationTx{atx1, atx2, atx3})
	assert.NoError(t, err)

	err = bl2.ProcessAtxs([]*types.ActivationTx{atx1})
	assert.NoError(t, err)

	block1 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block1.Signature = signer.Sign(block1.Bytes())
	block1.ATXID = *types.EmptyAtxId
	block2 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block2.Signature = signer.Sign(block2.Bytes())
	block2.AtxIds = append(block2.AtxIds, atx2.Id())
	block3 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block3.Signature = signer.Sign(block3.Bytes())
	block3.AtxIds = append(block3.AtxIds, atx3.Id())

	block2.CalcAndSetId()
	block3.CalcAndSetId()

	block1.AddView(block2.Id())
	block1.AddView(block3.Id())

	block1.CalcAndSetId()

	bl1.AddBlock(block1)
	bl1.AddBlock(block2)
	bl1.AddBlock(block3)

	_, err = bl1.GetBlock(block1.Id())
	if err != nil {
		t.Error(err)
	}

	bl2.syncLayer(0, []types.BlockID{block1.Id()})

	b, err := bl2.GetBlock(block1.Id())
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
	_, err = activation.SignAtx(signer, atx1)
	assert.NoError(t, err)
	// Push a block with tx1 and and atx1 into bl1.

	block := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block.Signature = signer.Sign(block.Bytes())
	block.TxIds = append(block.TxIds, tx1.Id())
	block.AtxIds = append(block.AtxIds, atx1.Id())
	err = bl1.AddBlockWithTxs(block, []*types.Transaction{tx1}, []*types.ActivationTx{atx1})
	require.NoError(t, err)

	_, err = bl1.GetBlock(block.Id())
	require.NoError(t, err)

	// Verify that bl2 doesn't have them in mempool.

	_, err = bl2.txpool.Get(tx1.Id())
	require.EqualError(t, err, "transaction not found in mempool")
	_, err = bl2.atxpool.Get(atx1.Id())
	require.EqualError(t, err, "cannot find ATX in mempool")

	// Sync bl2.

	txs, atxs, err := bl2.DataAvailability(block)
	require.NoError(t, err)
	require.Equal(t, 1, len(txs))
	require.Equal(t, tx1.Id(), txs[0].Id())
	require.Equal(t, 1, len(atxs))
	require.Equal(t, atx1.Id(), atxs[0].Id())

	// Verify that bl2 inserted them to the mempool.

	_, err = bl2.txpool.Get(tx1.Id())
	require.NoError(t, err)
	_, err = bl2.atxpool.Get(atx1.Id())
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
	block.TxIds = append(block.TxIds, tx1.Id())
	block.AtxIds = append(block.AtxIds, atx1.Id())
	err = bl1.AddBlockWithTxs(block, []*types.Transaction{}, []*types.ActivationTx{atx1})
	require.NoError(t, err)

	_, err = bl1.GetBlock(block.Id())
	require.NoError(t, err)
	// Sync bl2.

	_, _, err = bl2.DataAvailability(block)
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

	block1.AddView(block2.Id())
	block1.AddView(block3.Id())
	block1.AddView(block4.Id())
	block2.AddView(block5.Id())
	block2.AddView(block6.Id())
	block3.AddView(block6.Id())
	block4.AddView(block7.Id())
	block6.AddView(block7.Id())

	block1.AddVote(block2.Id())
	block1.AddVote(block3.Id())
	block2.AddVote(block5.Id())
	block3.AddVote(block6.Id())
	block4.AddVote(block7.Id())
	block6.AddVote(block7.Id())

	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "TestBlockListener_ValidateVotesGoodFlow", 2)
	defer bl1.Close()

	bl1.MeshDB.AddBlock(block1)
	bl1.MeshDB.AddBlock(block2)
	bl1.MeshDB.AddBlock(block3)
	bl1.MeshDB.AddBlock(block4)
	bl1.MeshDB.AddBlock(block5)
	bl1.MeshDB.AddBlock(block6)
	bl1.MeshDB.AddBlock(block7)
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

	block1.AddView(block2.Id())
	block1.AddView(block3.Id())
	//block1.AddView(4)
	block2.AddView(block5.Id())
	block2.AddView(block6.Id())
	block3.AddView(block6.Id())
	block4.AddView(block7.Id())
	block6.AddView(block7.Id())

	block1.AddVote(block2.Id())
	block1.AddVote(block3.Id())
	block1.AddVote(block4.Id())
	block2.AddVote(block5.Id())
	block3.AddVote(block6.Id())
	block4.AddVote(block7.Id())
	block6.AddVote(block7.Id())

	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "TestBlockListener_ValidateVotesBadFlow", 2)
	defer bl1.Close()
	bl1.MeshDB.AddBlock(block1)
	bl1.MeshDB.AddBlock(block2)
	bl1.MeshDB.AddBlock(block3)
	bl1.MeshDB.AddBlock(block4)
	bl1.MeshDB.AddBlock(block5)
	bl1.MeshDB.AddBlock(block6)
	bl1.MeshDB.AddBlock(block7)
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
	atx1.CalcAndSetId()

	err := bl1.ProcessAtxs([]*types.ActivationTx{atx})
	assert.NoError(t, err)
	err = bl2.ProcessAtxs([]*types.ActivationTx{&atx1})
	assert.NoError(t, err)

	block1 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block1.ATXID = *types.EmptyAtxId
	block1.Signature = signer.Sign(block1.Bytes())

	block2 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block2.ATXID = *types.EmptyAtxId
	block2.Signature = signer.Sign(block2.Bytes())

	block3 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block3.ATXID = *types.EmptyAtxId
	block3.Signature = signer.Sign(block3.Bytes())

	block4 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block4.ATXID = *types.EmptyAtxId
	block4.Signature = signer.Sign(block4.Bytes())

	block5 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block5.ATXID = *types.EmptyAtxId
	block5.Signature = signer.Sign(block5.Bytes())

	block6 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block6.ATXID = *types.EmptyAtxId
	block6.Signature = signer.Sign(block6.Bytes())

	block7 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block7.ATXID = *types.EmptyAtxId
	block7.Signature = signer.Sign(block7.Bytes())

	block8 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block8.ATXID = *types.EmptyAtxId
	block8.Signature = signer.Sign(block8.Bytes())

	block9 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block9.ATXID = *types.EmptyAtxId
	block9.Signature = signer.Sign(block9.Bytes())

	block10 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block10.ATXID = *types.EmptyAtxId
	block10.Signature = signer.Sign(block10.Bytes())

	block11 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block11.ATXID = *types.EmptyAtxId
	block11.Signature = signer.Sign(block11.Bytes())

	block1.CalcAndSetId()
	block2.AddView(block1.Id())
	block2.CalcAndSetId()
	block3.AddView(block2.Id())
	block3.CalcAndSetId()
	block4.AddView(block2.Id())
	block4.CalcAndSetId()
	block5.AddView(block3.Id())
	block5.AddView(block4.Id())
	block5.CalcAndSetId()
	block6.AddView(block4.Id())
	block6.CalcAndSetId()
	block7.AddView(block6.Id())
	block7.AddView(block5.Id())
	block7.CalcAndSetId()
	block8.AddView(block7.Id())
	block8.CalcAndSetId()
	block9.AddView(block5.Id())
	block9.CalcAndSetId()
	block10.AddView(block8.Id())
	block10.AddView(block9.Id())
	block10.CalcAndSetId()

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

	bl2.syncLayer(1, []types.BlockID{block10.Id(), block11.Id()})

	b, err := bl2.GetBlock(block10.Id())
	if err != nil {
		t.Error(err)
	}

	b, err = bl2.GetBlock(block11.Id())
	if err != nil {
		t.Error(err)
	}

	b, err = bl1.GetBlock(block1.Id())
	if err != nil {
		t.Error(err)
	}

	b, err = bl2.GetBlock(block1.Id())
	if err != nil {
		t.Error(err)
	}

	_, err = bl2.GetBlock(block2.Id())
	if err != nil {
		t.Error(err)
	}

	_, err = bl2.GetBlock(block3.Id())
	if err != nil {
		t.Error(err)
	}

	_, err = bl2.GetBlock(block4.Id())
	if err != nil {
		t.Error(err)
	}

	b, err = bl2.GetBlock(block5.Id())
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
	atx1.CalcAndSetId()

	err := bl1.ProcessAtxs([]*types.ActivationTx{atx})
	assert.NoError(t, err)
	err = bl2.ProcessAtxs([]*types.ActivationTx{&atx1})
	assert.NoError(t, err)

	block1 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block1.ATXID = *types.EmptyAtxId
	block1.Signature = signer.Sign(block1.Bytes())

	block2 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block2.ATXID = *types.EmptyAtxId
	block2.Signature = signer.Sign(block2.Bytes())

	block3 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block3.ATXID = *types.EmptyAtxId
	block3.Signature = signer.Sign(block3.Bytes())

	block4 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block4.ATXID = *types.EmptyAtxId
	block4.Signature = signer.Sign(block4.Bytes())

	block5 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block5.ATXID = *types.EmptyAtxId
	block5.Signature = signer.Sign(block5.Bytes())

	block6 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block6.ATXID = *types.EmptyAtxId
	block6.Signature = signer.Sign(block5.Bytes())

	block2.AddView(block1.Id())
	block3.AddView(block2.Id())
	block4.AddView(block2.Id())
	block5.AddView(block3.Id())
	block5.AddView(block4.Id())

	bl1.AddBlock(block2)
	bl1.AddBlock(block3)
	bl1.AddBlock(block4)
	bl1.AddBlock(block5)

	go bl2.syncLayer(5, []types.BlockID{block5.Id(), block6.Id()})
	time.Sleep(1 * time.Second) //wait for fetch

	b, err := bl2.GetBlock(block1.Id())
	assert.Error(t, err)

	_, err = bl2.GetBlock(block2.Id())
	assert.Error(t, err)

	_, err = bl2.GetBlock(block3.Id())
	assert.Error(t, err)

	_, err = bl2.GetBlock(block4.Id())
	assert.Error(t, err)

	b, err = bl2.GetBlock(block5.Id())
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
	_, err = activation.SignAtx(signer, atx)
	assert.NoError(t, err)

	blk := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	blk.TxIds = append(blk.TxIds, tx.Id())
	blk.AtxIds = append(blk.AtxIds, atx.Id())
	blk.Signature = signer.Sign(blk.Bytes())
	blk.CalcAndSetId()

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
			if b, err := bl1.GetBlock(blk.Id()); err == nil {
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
	bl1.AtxDB = atxDb

	defer bl1.Close()

	atx1 := atx(signer.PublicKey().String())
	atx2 := atx(signer.PublicKey().String())

	// Push block with tx1 and and atx1 into bl1.
	blk1 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	blk1.Signature = signer.Sign(blk1.Bytes())
	blk1.TxIds = append(blk1.TxIds, tx1.Id())
	blk1.AtxIds = append(blk1.AtxIds, atx1.Id())
	blk1.AtxIds = append(blk1.AtxIds, atx2.Id())

	err := bl1.AddBlockWithTxs(blk1, []*types.Transaction{tx1}, []*types.ActivationTx{atx1, atx2})
	require.NoError(t, err)
	require.Equal(t, 2, atxDb.ProcCnt)
	_, err = bl1.GetBlock(blk1.Id())
	require.NoError(t, err)

	// Push different block with same transactions - not expected to process atxs
	blk2 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	blk2.Signature = signer.Sign(blk2.Bytes())
	blk2.TxIds = append(blk2.TxIds, tx1.Id())
	blk2.AtxIds = append(blk2.AtxIds, atx1.Id())
	blk2.AtxIds = append(blk2.AtxIds, atx2.Id())

	err = bl1.AddBlockWithTxs(blk2, []*types.Transaction{tx1}, []*types.ActivationTx{atx1, atx2})
	require.NoError(t, err)
	require.Equal(t, 4, atxDb.ProcCnt)
	_, err = bl1.GetBlock(blk2.Id())
	require.NoError(t, err)

	// Push different block with subset of transactions - expected to process atxs
	blk3 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	blk3.Signature = signer.Sign(blk3.Bytes())
	blk3.TxIds = append(blk3.TxIds, tx1.Id())
	blk3.AtxIds = append(blk3.AtxIds, atx1.Id())

	err = bl1.AddBlockWithTxs(blk3, []*types.Transaction{tx1}, []*types.ActivationTx{atx1})
	require.NoError(t, err)
	require.Equal(t, 5, atxDb.ProcCnt)
	_, err = bl1.GetBlock(blk3.Id())
	require.NoError(t, err)
}

//todo integration testing
