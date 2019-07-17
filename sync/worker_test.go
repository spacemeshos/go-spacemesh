package sync

import (
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"sync"
	"testing"
	"time"
)

func TestNewPeerWorker(t *testing.T) {
	syncs, nodes := SyncMockFactory(4, conf, "TestNewPeerWorker", memoryDB, newMockPoetDb)
	syncObj1 := syncs[0]
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	defer syncObj2.Close()
	lid := types.LayerID(1)
	err := syncObj1.AddBlock(types.NewExistingBlock(types.BlockID(123), lid, nil))
	assert.NoError(t, err)

	wrk, output := NewPeersWorker(syncObj2, []p2p.Peer{nodes[3].PublicKey(), nodes[2].PublicKey(), nodes[0].PublicKey()}, &sync.Once{}, LayerIdsReqFactory(1))

	go wrk.Work()
	wrk.Wait()

	timeout := time.NewTimer(1 * time.Second)
	select {
	case item := <-output:
		assert.Equal(t, types.BlockID(123), item.([]types.BlockID)[0], "wrong ids")
	case <-timeout.C:
		assert.Fail(t, "no message received on channel")
	}

}

func TestNewNeighborhoodWorker(t *testing.T) {
	syncs, nodes := SyncMockFactory(2, conf, "TestNewNeighborhoodWorker", memoryDB, newMockPoetDb)
	syncObj1 := syncs[0]
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	defer syncObj2.Close()

	block := types.NewExistingBlock(types.BlockID(333), 1, nil)
	syncObj1.AddBlockWithTxs(block, []*types.AddressableSignedTransaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	timeout := time.NewTimer(2 * time.Second)
	pm1 := getPeersMock([]p2p.Peer{nodes[0].PublicKey()})
	syncObj2.Peers = pm1

	id1 := types.GetTransactionId(tx1.SerializableSignedTransaction)
	id2 := types.GetTransactionId(tx2.SerializableSignedTransaction)
	id3 := types.GetTransactionId(tx3.SerializableSignedTransaction)

	wrk := NewNeighborhoodWorker(syncObj2, 1, TxReqFactory([]types.TransactionId{id1, id2, id3}, syncObj2))
	go wrk.Work()

	select {
	case item := <-wrk.output:
		txs := item.([]types.SerializableSignedTransaction)
		assert.Equal(t, 3, len(txs))
		mp := make(map[types.TransactionId]struct{})
		mp[types.GetTransactionId(&txs[0])] = struct{}{}
		mp[types.GetTransactionId(&txs[1])] = struct{}{}
		mp[types.GetTransactionId(&txs[2])] = struct{}{}

		_, ok := mp[id1]
		assert.True(t, ok)
		_, ok = mp[id2]
		assert.True(t, ok)
		_, ok = mp[id3]
		assert.True(t, ok)

	case <-timeout.C:
		assert.Fail(t, "no message received on channel")
	}
	wrk.Wait()
}

func TestNewBlockWorker(t *testing.T) {
	syncs, nodes := SyncMockFactory(3, conf, "TestNewNeighborhoodWorker", memoryDB, newMockPoetDb)
	syncObj1 := syncs[0]
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	defer syncObj2.Close()
	syncObj3 := syncs[2]
	defer syncObj3.Close()

	block := types.NewExistingBlock(types.BlockID(333), 1, nil)
	syncObj1.AddBlockWithTxs(block, []*types.AddressableSignedTransaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj2.AddBlockWithTxs(block, []*types.AddressableSignedTransaction{tx1, tx2, tx3}, []*types.ActivationTx{})

	timeout := time.NewTimer(10 * time.Second)
	pm1 := getPeersMock([]p2p.Peer{nodes[0].PublicKey(), nodes[1].PublicKey()})
	syncObj3.Peers = pm1

	wrk := NewBlockWorker(syncObj3, 1, BlockReqFactory(), blockSliceToChan([]types.BlockID{block.Id}))
	go wrk.Work()
	count := 0
loop:
	for {
		select {
		case item, ok := <-wrk.output:
			if !ok {
				break loop
			}

			assert.True(t, item.(*types.Block).ID() == block.ID())
			count++
		case <-timeout.C:

		}
	}

	wrk.Wait()
	assert.True(t, count == 1)
}
