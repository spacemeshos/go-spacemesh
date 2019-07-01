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
	syncs, nodes := SyncMockFactory(4, conf, "TestNewPeerWorker", memoryDB)
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
	syncs, nodes := SyncMockFactory(2, conf, "TestNewNeighborhoodWorker", memoryDB)
	syncObj1 := syncs[0]
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	defer syncObj2.Close()

	block := types.NewExistingBlock(types.BlockID(333), 1, nil)
	syncObj1.AddBlockWithTxs(block, []*types.SerializableTransaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	timeout := time.NewTimer(2 * time.Second)
	pm1 := getPeersMock([]p2p.Peer{nodes[0].PublicKey()})
	syncObj2.Peers = pm1

	wrk := NewNeighborhoodWorker(syncObj2, 1, TxReqFactory([]types.TransactionId{types.GetTransactionId(tx1), types.GetTransactionId(tx2), types.GetTransactionId(tx3)}))
	go wrk.Work()

	select {
	case item := <-wrk.output:
		txs := item.([]types.SerializableTransaction)
		assert.Equal(t, 3, len(txs))
		mp := make(map[types.TransactionId]struct{})
		mp[types.GetTransactionId(&txs[0])] = struct{}{}
		mp[types.GetTransactionId(&txs[1])] = struct{}{}
		mp[types.GetTransactionId(&txs[2])] = struct{}{}

		_, ok := mp[types.GetTransactionId(tx1)]
		assert.True(t, ok)
		_, ok = mp[types.GetTransactionId(tx2)]
		assert.True(t, ok)
		_, ok = mp[types.GetTransactionId(tx3)]
		assert.True(t, ok)

	case <-timeout.C:
		assert.Fail(t, "no message received on channel")
	}
	wrk.Wait()
}
