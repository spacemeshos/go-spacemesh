package sync

import (
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/types"
	"testing"
	"time"
)

func TestBlockListener_TestTxQueue(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	//n2.RegisterGossipProtocol(NewBlockProtocol)

	bl1 := SyncFactory("TextTxQueue_1", n1)
	bl1.Peers = PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}
	bl2 := SyncFactory("TextTxQueue_2", n2)
	bl2.Peers = PeersMock{func() []p2p.Peer { return []p2p.Peer{n1.PublicKey()} }}

	bl1.Start()
	bl2.Start()
	queue := NewTxQueue(bl1)
	id1 := types.GetTransactionId(tx1.SerializableSignedTransaction)
	id2 := types.GetTransactionId(tx2.SerializableSignedTransaction)
	id3 := types.GetTransactionId(tx3.SerializableSignedTransaction)

	//missing
	id4 := types.GetTransactionId(tx4.SerializableSignedTransaction)

	block1 := types.NewExistingBlock(types.BlockID(111), 1, nil)
	block1.TxIds = []types.TransactionId{id1, id2, id3}
	bl2.AddBlockWithTxs(block1, []*types.AddressableSignedTransaction{tx1, tx2, tx3}, []*types.ActivationTx{})

	ch := queue.addToPending([]types.TransactionId{id1, id2, id3})
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

	ch = queue.addToPending([]types.TransactionId{id1, id2, id3, id4})
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

	bl2.Close()
	bl1.Close()
	time.Sleep(1 * time.Second)
}

func TestBlockListener_TestAtxQueue(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	//n2.RegisterGossipProtocol(NewBlockProtocol)

	bl1 := SyncFactory("TextAtxQueue_1", n1)
	bl1.Peers = PeersMock{func() []p2p.Peer { return []p2p.Peer{n2.PublicKey()} }}
	bl2 := SyncFactory("TextAtxQueue_2", n2)
	bl2.Peers = PeersMock{func() []p2p.Peer { return []p2p.Peer{n1.PublicKey()} }}

	bl1.Start()
	bl2.Start()
	queue := NewAtxQueue(bl1)

	block1 := types.NewExistingBlock(types.BlockID(111), 1, nil)
	atx1 := atx()
	atx2 := atx()
	atx3 := atx()
	atx4 := atx()

	bl2.AddBlockWithTxs(block1, []*types.AddressableSignedTransaction{}, []*types.ActivationTx{atx1, atx2, atx3})

	ch := queue.addToPending([]types.AtxId{atx1.Id(), atx2.Id(), atx3.Id()})
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

	ch = queue.addToPending([]types.AtxId{atx1.Id(), atx2.Id(), atx3.Id(), atx4.Id()})
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

	bl2.Close()
	bl1.Close()
	time.Sleep(1 * time.Second)
}
