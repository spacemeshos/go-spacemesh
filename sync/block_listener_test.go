package sync

import (
	"errors"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
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

type mockTxProcessor struct {
	notValid bool
}

func (m mockTxProcessor) GetValidAddressableTx(tx *types.SerializableSignedTransaction) (*types.AddressableSignedTransaction, error) {
	addr, err := m.ValidateTransactionSignature(tx)
	if err != nil {
		return nil, err
	}

	return &types.AddressableSignedTransaction{SerializableSignedTransaction: tx, Address: addr}, nil
}

func (m mockTxProcessor) ValidateTransactionSignature(tx *types.SerializableSignedTransaction) (types.Address, error) {
	if !m.notValid {
		return types.HexToAddress("0xFFFF"), nil
	}
	return types.Address{}, errors.New("invalid sig for tx")
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
	sync := NewSync(serv, getMesh(memoryDB, name), miner.NewTypesTransactionIdMemPool(), miner.NewTypesAtxIdMemPool(), mockTxProcessor{}, blockValidator, poetDb, conf, ts, l)
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
	atx1 := atx()

	atx2 := atx()
	atx3 := atx()

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

	_, err = types.SignAtx(signer, atx1)
	assert.NoError(t, err)
	bl1.ProcessAtx(atx1)
	_, err = types.SignAtx(signer, atx2)
	assert.NoError(t, err)
	bl1.ProcessAtx(atx2)
	_, err = types.SignAtx(signer, atx3)
	assert.NoError(t, err)
	bl1.ProcessAtx(atx3)

	bl2.ProcessAtx(atx1)

	block1 := types.NewExistingBlock(types.BlockID(123), 1, nil)
	block1.Signature = signer.Sign(block1.Bytes())
	block1.ATXID = *types.EmptyAtxId
	block2 := types.NewExistingBlock(types.BlockID(321), 0, nil)
	block2.Signature = signer.Sign(block2.Bytes())
	block2.AtxIds = append(block2.AtxIds, atx2.Id())
	block3 := types.NewExistingBlock(types.BlockID(222), 0, nil)
	block3.Signature = signer.Sign(block3.Bytes())
	block3.AtxIds = append(block3.AtxIds, atx3.Id())

	block1.AddView(block2.ID())
	block1.AddView(block3.ID())

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

	atx1 := atx()

	// Push atx1 poet proof into bl1.

	proofMessage := makePoetProofMessage(t)
	err := bl1.poetDb.ValidateAndStore(&proofMessage)
	require.NoError(t, err)

	poetProofBytes, err := types.InterfaceToBytes(&proofMessage.PoetProof)
	require.NoError(t, err)
	poetRef := sha256.Sum256(poetProofBytes)

	atx1.Nipst.PostProof.Challenge = poetRef[:]
	_, err = types.SignAtx(signer, atx1)
	assert.NoError(t, err)
	// Push a block with tx1 and and atx1 into bl1.

	block := types.NewExistingBlock(types.BlockID(2), 1, nil)
	block.Signature = signer.Sign(block.Bytes())
	block.TxIds = append(block.TxIds, types.GetTransactionId(tx1.SerializableSignedTransaction))
	block.AtxIds = append(block.AtxIds, atx1.Id())
	err = bl1.AddBlockWithTxs(block, []*types.AddressableSignedTransaction{tx1}, []*types.ActivationTx{atx1})
	require.NoError(t, err)

	_, err = bl1.GetBlock(block.ID())
	require.NoError(t, err)

	// Verify that bl2 doesn't have them in mempool.

	_, err = bl2.txpool.Get(types.GetTransactionId(tx1.SerializableSignedTransaction))
	require.Error(t, err)
	_, err = bl2.atxpool.Get(atx1.Id())
	require.Error(t, err)

	// Sync bl2.

	txs, atxs, err := bl2.DataAvailabilty(block)
	require.NoError(t, err)
	require.Equal(t, 1, len(txs))
	require.Equal(t, types.GetTransactionId(tx1.SerializableSignedTransaction), types.GetTransactionId(txs[0].SerializableSignedTransaction))
	require.Equal(t, 1, len(atxs))
	require.Equal(t, atx1.Id(), atxs[0].Id())

	// Verify that bl2 inserted them to the mempool.

	_, err = bl2.txpool.Get(types.GetTransactionId(tx1.SerializableSignedTransaction))
	require.NoError(t, err)
	_, err = bl2.atxpool.Get(atx1.Id())
	require.NoError(t, err)
}

func TestBlockListener_ValidateVotesGoodFlow(t *testing.T) {
	block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block1.Id = 1

	block2 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block2.Id = 2

	block3 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block3.Id = 3

	block4 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block4.Id = 4

	block5 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block5.Id = 5

	block6 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block6.Id = 6

	block7 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block7.Id = 7

	block1.AddView(2)
	block1.AddView(3)
	block1.AddView(4)
	block2.AddView(5)
	block2.AddView(6)
	block3.AddView(6)
	block4.AddView(7)
	block6.AddView(7)

	block1.AddVote(2)
	block1.AddVote(3)
	block2.AddVote(5)
	block3.AddVote(6)
	block4.AddVote(7)
	block6.AddVote(7)

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

	assert.True(t, validateVotes(block1, bl1.ForBlockInView, bl1.Hdist))
}

func TestBlockListener_ValidateVotesBadFlow(t *testing.T) {
	block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 7, []byte("data data data"))
	block1.Id = 1

	block2 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 8, []byte("data data data"))
	block2.Id = 2

	block3 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 8, []byte("data data data"))
	block3.Id = 3

	block4 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 9, []byte("data data data"))
	block4.Id = 4

	block5 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 9, []byte("data data data"))
	block5.Id = 5

	block6 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 9, []byte("data data data"))
	block6.Id = 6

	block7 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 10, []byte("data data data"))
	block7.Id = 7

	block1.AddView(2)
	block1.AddView(3)
	//block1.AddView(4)
	block2.AddView(5)
	block2.AddView(6)
	block3.AddView(6)
	block4.AddView(7)
	block6.AddView(7)

	block1.AddVote(2)
	block1.AddVote(3)
	block1.AddVote(4)
	block2.AddVote(5)
	block3.AddVote(6)
	block4.AddVote(7)
	block6.AddVote(7)

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

	assert.False(t, validateVotes(block1, bl1.ForBlockInView, bl1.Hdist))
}

func TestBlockListenerViewTraversal(t *testing.T) {

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

	atx := atx()

	byts, _ := types.InterfaceToBytes(atx)
	var atx1 types.ActivationTx
	types.BytesToInterface(byts, &atx1)
	atx1.CalcAndSetId()

	bl1.ProcessAtx(atx)
	bl2.ProcessAtx(&atx1)

	block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block1.ATXID = *types.EmptyAtxId
	block1.Signature = signer.Sign(block1.Bytes())
	block1.Id = 1

	block2 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block2.ATXID = *types.EmptyAtxId
	block2.Signature = signer.Sign(block2.Bytes())
	block2.Id = 2

	block3 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block3.ATXID = *types.EmptyAtxId
	block3.Signature = signer.Sign(block3.Bytes())
	block3.Id = 3

	block4 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block4.ATXID = *types.EmptyAtxId
	block4.Signature = signer.Sign(block4.Bytes())
	block4.Id = 4

	block5 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block5.ATXID = *types.EmptyAtxId
	block5.Signature = signer.Sign(block5.Bytes())
	block5.Id = 5

	block6 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block6.ATXID = *types.EmptyAtxId
	block6.Signature = signer.Sign(block6.Bytes())
	block6.Id = 6

	block7 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block7.ATXID = *types.EmptyAtxId
	block7.Signature = signer.Sign(block7.Bytes())
	block7.Id = 7

	block8 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block8.ATXID = *types.EmptyAtxId
	block8.Signature = signer.Sign(block8.Bytes())
	block8.Id = 8

	block9 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block9.ATXID = *types.EmptyAtxId
	block9.Signature = signer.Sign(block9.Bytes())
	block9.Id = 9

	block10 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block10.ATXID = *types.EmptyAtxId
	block10.Signature = signer.Sign(block10.Bytes())
	block10.Id = 10

	block11 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block11.ATXID = *types.EmptyAtxId
	block11.Signature = signer.Sign(block11.Bytes())
	block11.Id = 11

	block2.AddView(block1.ID())
	block3.AddView(block2.ID())
	block4.AddView(block2.ID())
	block5.AddView(block3.ID())
	block5.AddView(block4.ID())
	block6.AddView(block4.ID())
	block7.AddView(block6.ID())
	block7.AddView(block5.ID())
	block8.AddView(block7.ID())
	block9.AddView(block5.ID())
	block10.AddView(block8.ID())
	block10.AddView(block9.ID())

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
	bl1.AddBlock(block11)

	bl2.syncLayer(1, []types.BlockID{block10.Id, block11.Id})

	b, err := bl1.GetBlock(block1.Id)
	if err != nil {
		t.Error(err)
	}

	b, err = bl2.GetBlock(block1.Id)
	if err != nil {
		t.Error(err)
	}

	_, err = bl2.GetBlock(block2.Id)
	if err != nil {
		t.Error(err)
	}

	_, err = bl2.GetBlock(block3.Id)
	if err != nil {
		t.Error(err)
	}

	_, err = bl2.GetBlock(block4.Id)
	if err != nil {
		t.Error(err)
	}

	b, err = bl2.GetBlock(block5.Id)
	if err != nil {
		t.Error(err)
	}

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

	atx := atx()

	byts, _ := types.InterfaceToBytes(atx)
	var atx1 types.ActivationTx
	types.BytesToInterface(byts, &atx1)
	atx1.CalcAndSetId()

	bl1.ProcessAtx(atx)
	bl2.ProcessAtx(&atx1)

	block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block1.Id = 1
	block1.ATXID = *types.EmptyAtxId
	block1.Signature = signer.Sign(block1.Bytes())

	block2 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block2.ATXID = *types.EmptyAtxId
	block2.Signature = signer.Sign(block2.Bytes())
	block2.Id = 2

	block3 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block3.ATXID = *types.EmptyAtxId
	block3.Signature = signer.Sign(block3.Bytes())
	block3.Id = 3

	block4 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block4.ATXID = *types.EmptyAtxId
	block4.Signature = signer.Sign(block4.Bytes())
	block4.Id = 4

	block5 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block5.ATXID = *types.EmptyAtxId
	block5.Signature = signer.Sign(block5.Bytes())
	block5.Id = 5

	block6 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block6.ATXID = *types.EmptyAtxId
	block6.Signature = signer.Sign(block5.Bytes())
	block6.Id = 6

	block2.AddView(block1.ID())
	block3.AddView(block2.ID())
	block4.AddView(block2.ID())
	block5.AddView(block3.ID())
	block5.AddView(block4.ID())

	bl1.AddBlock(block2)
	bl1.AddBlock(block3)
	bl1.AddBlock(block4)
	bl1.AddBlock(block5)

	go bl2.syncLayer(5, []types.BlockID{block5.Id, block6.Id})
	time.Sleep(1 * time.Second) //wait for fetch

	b, err := bl2.GetBlock(block1.Id)
	assert.Error(t, err)

	_, err = bl2.GetBlock(block2.Id)
	assert.Error(t, err)

	_, err = bl2.GetBlock(block3.Id)
	assert.Error(t, err)

	_, err = bl2.GetBlock(block4.Id)
	assert.Error(t, err)

	b, err = bl2.GetBlock(block5.Id)
	assert.Error(t, err)

	t.Log("  ", b)
	t.Log("done!")
}

func TestBlockListener_ListenToGossipBlocks(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	//n2.RegisterGossipProtocol(NewBlockProtocol)

	bl1 := ListenerFactory(n1, PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}, "TestBlockListener_ListenToGossipBlocks1", 1)
	bl2 := ListenerFactory(n2, PeersMock{func() []p2p.Peer { return []p2p.Peer{n1.PublicKey()} }}, "TestBlockListener_ListenToGossipBlocks2", 1)

	bl1.Start()
	bl2.Start()

	blk := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data1"))
	blk.ViewEdges = append(blk.ViewEdges, mesh.GenesisBlock.ID())
	tx := types.NewAddressableTx(0, types.BytesToAddress([]byte{0x01}), types.BytesToAddress([]byte{0x02}), 10, 10, 10)
	signer := signing.NewEdSigner()
	atx := atx()
	_, err := types.SignAtx(signer, atx)
	assert.NoError(t, err)
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

	bl2.AddBlockWithTxs(blk, []*types.AddressableSignedTransaction{tx}, []*types.ActivationTx{atx})

	mblk := types.Block{MiniBlock: types.MiniBlock{BlockHeader: blk.BlockHeader, TxIds: []types.TransactionId{types.GetTransactionId(tx.SerializableSignedTransaction)}, AtxIds: []types.AtxId{atx.Id()}}}
	mblk.Signature = signer.Sign(mblk.Bytes())

	data, err := types.InterfaceToBytes(&mblk)
	require.NoError(t, err)

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
			if b, err := bl1.GetBlock(blk.Id); err == nil {
				assert.True(t, mblk.Compare(b))
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

	atx1 := atx()
	atx2 := atx()

	// Push block with tx1 and and atx1 into bl1.
	blk1 := types.NewExistingBlock(types.BlockID(1), 1, nil)
	blk1.Signature = signer.Sign(blk1.Bytes())
	blk1.TxIds = append(blk1.TxIds, types.GetTransactionId(tx1.SerializableSignedTransaction))
	blk1.AtxIds = append(blk1.AtxIds, atx1.Id())
	blk1.AtxIds = append(blk1.AtxIds, atx2.Id())

	err := bl1.AddBlockWithTxs(blk1, []*types.AddressableSignedTransaction{tx1}, []*types.ActivationTx{atx1, atx2})
	require.NoError(t, err)
	require.Equal(t, 2, atxDb.ProcCnt)
	_, err = bl1.GetBlock(blk1.Id)
	require.NoError(t, err)

	// Push different block with same transactions - not expected to process atxs
	blk2 := types.NewExistingBlock(types.BlockID(2), 1, nil)
	blk2.Signature = signer.Sign(blk2.Bytes())
	blk2.TxIds = append(blk2.TxIds, types.GetTransactionId(tx1.SerializableSignedTransaction))
	blk2.AtxIds = append(blk2.AtxIds, atx1.Id())
	blk2.AtxIds = append(blk2.AtxIds, atx2.Id())

	err = bl1.AddBlockWithTxs(blk2, []*types.AddressableSignedTransaction{tx1}, []*types.ActivationTx{atx1, atx2})
	require.NoError(t, err)
	require.Equal(t, 4, atxDb.ProcCnt)
	_, err = bl1.GetBlock(blk2.Id)
	require.NoError(t, err)

	// Push different block with subset of transactions - expected to process atxs
	blk3 := types.NewExistingBlock(types.BlockID(3), 1, nil)
	blk3.Signature = signer.Sign(blk3.Bytes())
	blk3.TxIds = append(blk3.TxIds, types.GetTransactionId(tx1.SerializableSignedTransaction))
	blk3.AtxIds = append(blk3.AtxIds, atx1.Id())

	err = bl1.AddBlockWithTxs(blk3, []*types.AddressableSignedTransaction{tx1}, []*types.ActivationTx{atx1})
	require.NoError(t, err)
	require.Equal(t, 5, atxDb.ProcCnt)
	_, err = bl1.GetBlock(blk3.Id)
	require.NoError(t, err)
}

//todo integration testing
