package sync

import (
	"crypto/sha256"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	wrk, output := NewPeersWorker(syncObj2.MessageServer, []p2p.Peer{nodes[3].PublicKey(), nodes[2].PublicKey(), nodes[0].PublicKey()}, &sync.Once{}, LayerIdsReqFactory(1))

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
	r := require.New(t)
	syncs, nodes := SyncMockFactory(2, conf, "TestSyncer_FetchPoetProofAvailableAndValid_", memoryDB, newMemPoetDb)
	s0 := syncs[0]
	s1 := syncs[1]
	s1.Peers = getPeersMock([]p2p.Peer{nodes[0].PublicKey()})

	proofMessage := makePoetProofMessage(t)

	err := s0.poetDb.ValidateAndStore(&proofMessage)
	r.NoError(err)

	poetProofBytes, err := types.InterfaceToBytes(&proofMessage.PoetProof)
	r.NoError(err)
	ref := sha256.Sum256(poetProofBytes)

	w := NewNeighborhoodWorker(s1.MessageServer, 1, PoetReqFactory(ref[:]))
	go w.work()
	assert.NotNil(t, <-w.output)
	r.NoError(err)
	w.Wait()
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

	wrk := NewBlockWorker(syncObj3.MessageServer, 1, BlockReqFactory(), blockSliceToChan([]types.BlockID{block.Id}))
	go wrk.Work()
	count := 0
loop:
	for {
		select {
		case item, ok := <-wrk.output:
			if !ok {
				break loop
			}

			assert.True(t, item.(blockJob).id == block.ID())
			count++
		case <-timeout.C:

		}
	}

	wrk.Wait()
	assert.True(t, count == 1)
}
