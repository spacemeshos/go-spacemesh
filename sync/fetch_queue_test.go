package sync

import (
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/state"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"testing"
	"time"

	"github.com/spacemeshos/sha256-simd"
	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	p2ppeers "github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type PeersMock struct {
	getPeers func() []p2ppeers.Peer
}

func (pm PeersMock) GetPeers() []p2ppeers.Peer {
	return pm.getPeers()
}

func (pm PeersMock) Close() {
	return
}

func init() {
	types.SetLayersPerEpoch(4)
}

func SyncFactory(name string, serv service.Service) *Syncer {
	tick := 20 * time.Second
	ts := timesync.NewClock(timesync.RealClock{}, tick, time.Now(), log.NewDefault("clock"))
	l := log.NewDefault(name)
	poetDb := activation.NewPoetDb(database.NewMemDatabase(), l.WithName("poetDb"))
	blockValidator := blockEligibilityValidatorMock{}
	sync := NewSync(serv, getMesh(memoryDB, name, nil), state.NewTxMemPool(), activation.NewAtxMemPool(), blockValidator, poetDb, conf, ts, nil, l)
	return sync
}

func TestBlockListener_TestTxQueue(t *testing.T) {
	t.Skip()
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	//n2.RegisterGossipProtocol(NewBlockProtocol)

	bl1 := SyncFactory("TextTxQueue_1", n1)
	bl1.peers = PeersMock{func() []p2ppeers.Peer { return []p2ppeers.Peer{n2.PublicKey()} }}
	bl2 := SyncFactory("TextTxQueue_2", n2)
	bl2.peers = PeersMock{func() []p2ppeers.Peer { return []p2ppeers.Peer{n1.PublicKey()} }}

	bl1.Start()
	bl2.Start()
	queue := bl1.txQueue
	id1 := tx1.ID()
	id2 := tx2.ID()
	id3 := tx3.ID()

	//missing
	id4 := tx4.ID()

	block1 := types.NewExistingBlock(1, []byte(rand.String(8)), nil)
	block1.TxIDs = []types.TransactionID{id1, id2, id3}
	block1.Initialize()
	addTxsToPool(bl2.txpool, []*types.Transaction{tx1, tx2, tx3})
	bl2.AddBlockWithTxs(block1)

	ch := queue.addToPendingGetCh([]types.Hash32{id1.Hash32(), id2.Hash32(), id3.Hash32()})
	timeout := time.After(1 * time.Second)

	select {
	// Got a timeout! fail with a timeout error
	case <-timeout:
		t.Error("timed out ")
		return
	case <-ch:
		t.Log("done!")
		break
	}

	ch = queue.addToPendingGetCh([]types.Hash32{id1.Hash32(), id2.Hash32(), id3.Hash32(), id4.Hash32()})
	timeout = time.After(1 * time.Second)

	select {
	// Got a timeout! fail with a timeout error
	case <-timeout:
		t.Error("timed out ")
		return
	case done := <-ch:
		if done {
			t.Error("done! without fetching")
		}
	}

	assert.True(t, len(queue.pending) == 0)

	bl2.Close()
	bl1.Close()
	time.Sleep(1 * time.Second)
}

func TestBlockListener_TestAtxQueue(t *testing.T) {
	t.Skip()
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	signer := signing.NewEdSigner()

	bl1 := SyncFactory("TextAtxQueue_1", n1)
	bl1.peers = PeersMock{func() []p2ppeers.Peer { return []p2ppeers.Peer{n2.PublicKey()} }}
	bl2 := SyncFactory("TextAtxQueue_2", n2)
	bl2.peers = PeersMock{func() []p2ppeers.Peer { return []p2ppeers.Peer{n1.PublicKey()} }}

	bl1.Start()
	bl2.Start()
	queue := bl1.atxQueue

	block1 := types.NewExistingBlock(1, []byte(rand.String(8)), nil)
	atx1 := atx(signer.PublicKey().String())
	atx2 := atx(signer.PublicKey().String())
	atx3 := atx(signer.PublicKey().String())
	atx4 := atx(signer.PublicKey().String())

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
	err = activation.SignAtx(signer, atx1)
	assert.NoError(t, err)
	atx2.Nipst.PostProof.Challenge = poetRef[:]
	err = activation.SignAtx(signer, atx2)
	assert.NoError(t, err)
	atx3.Nipst.PostProof.Challenge = poetRef[:]
	err = activation.SignAtx(signer, atx3)
	assert.NoError(t, err)
	atx4.Nipst.PostProof.Challenge = poetRef[:]
	err = activation.SignAtx(signer, atx4)
	assert.NoError(t, err)

	err = bl1.ProcessAtxs([]*types.ActivationTx{atx1})
	assert.NoError(t, err)

	bl2.atxDb.ProcessAtx(atx1)
	bl2.atxDb.ProcessAtx(atx2)
	bl2.atxDb.ProcessAtx(atx3)
	bl2.AddBlockWithTxs(block1)

	ch := queue.addToPendingGetCh([]types.Hash32{atx1.Hash32(), atx2.Hash32(), atx3.Hash32()})
	timeout := time.After(1 * time.Second)
	select {
	// Got a timeout! fail with a timeout error
	case <-timeout:
		t.Error("timed out ")
		return
	case <-ch:
		t.Log("done!")
		break
	}

	ch = queue.addToPendingGetCh([]types.Hash32{atx1.Hash32(), atx2.Hash32(), atx3.Hash32(), atx4.Hash32()})
	timeout = time.After(1 * time.Second)

	select {
	// Got a timeout! fail with a timeout error
	case <-timeout:
		t.Error("timed out ")
		return
	case done := <-ch:
		if done {
			t.Error("done! without fetching")
		}
	}

	assert.True(t, len(queue.pending) == 0)

	bl2.Close()
	bl1.Close()
	time.Sleep(1 * time.Second)
}

func TestBlockListener_TestTxQueueHandle(t *testing.T) {
	t.Skip()
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	//n2.RegisterGossipProtocol(NewBlockProtocol)

	bl1 := SyncFactory("TextTxQueueHandle_1", n1)
	bl1.peers = PeersMock{func() []p2ppeers.Peer { return []p2ppeers.Peer{n2.PublicKey()} }}
	bl2 := SyncFactory("TextTxQueueHandle_2", n2)
	bl2.peers = PeersMock{func() []p2ppeers.Peer { return []p2ppeers.Peer{n1.PublicKey()} }}

	bl1.Start()
	bl2.Start()
	queue := bl1.txQueue
	id1 := tx1.ID()
	id2 := tx2.ID()
	id3 := tx3.ID()

	block1 := types.NewExistingBlock(1, []byte(rand.String(8)), nil)
	block1.TxIDs = []types.TransactionID{id1, id2, id3}
	addTxsToPool(bl2.txpool, []*types.Transaction{tx1, tx2, tx3})
	bl2.AddBlockWithTxs(block1)

	res, err := queue.handle([]types.Hash32{id1.Hash32(), id2.Hash32(), id3.Hash32()})
	if err != nil {
		t.Error(err)
	}

	if len(res) != 3 {
		t.Error("wrong length")
	}

	assert.True(t, len(queue.pending) == 0)

	bl2.Close()
	bl1.Close()
	time.Sleep(1 * time.Second)
}

func TestBlockListener_TestAtxQueueHandle(t *testing.T) {
	t.Skip()
	sim := service.NewSimulator()
	signer := signing.NewEdSigner()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	//n2.RegisterGossipProtocol(NewBlockProtocol)

	bl1 := SyncFactory("TextAtxQueueHandle_1", n1)
	bl1.peers = PeersMock{func() []p2ppeers.Peer { return []p2ppeers.Peer{n2.PublicKey()} }}
	bl2 := SyncFactory("TextAtxQueueHandle_2", n2)
	bl2.peers = PeersMock{func() []p2ppeers.Peer { return []p2ppeers.Peer{n1.PublicKey()} }}

	bl1.Start()
	bl2.Start()

	proofMessage := makePoetProofMessage(t)
	err := bl2.poetDb.ValidateAndStore(&proofMessage)

	poetProofBytes, err := types.InterfaceToBytes(&proofMessage.PoetProof)
	poetRef := sha256.Sum256(poetProofBytes)

	block1 := types.NewExistingBlock(1, []byte(rand.String(8)), nil)
	atx1 := atx(signer.PublicKey().String())
	atx1.Nipst.PostProof.Challenge = poetRef[:]
	err = activation.SignAtx(signer, atx1)
	assert.NoError(t, err)
	atx2 := atx(signer.PublicKey().String())
	atx2.Nipst.PostProof.Challenge = poetRef[:]
	err = activation.SignAtx(signer, atx2)
	assert.NoError(t, err)
	atx3 := atx(signer.PublicKey().String())
	atx3.Nipst.PostProof.Challenge = poetRef[:]
	err = activation.SignAtx(signer, atx3)
	assert.NoError(t, err)

	bl2.atxDb.ProcessAtx(atx1)
	bl2.atxDb.ProcessAtx(atx2)
	bl2.atxDb.ProcessAtx(atx3)
	bl2.AddBlockWithTxs(block1)

	res, err := bl1.atxQueue.handle([]types.Hash32{atx1.Hash32(), atx2.Hash32(), atx3.Hash32()})
	if err != nil {
		t.Error(err)
	}

	if len(res) != 3 {
		t.Error("wrong length")
	}

	assert.True(t, len(bl1.atxQueue.pending) == 0)

	bl2.Close()
	bl1.Close()
	time.Sleep(1 * time.Second)
}
