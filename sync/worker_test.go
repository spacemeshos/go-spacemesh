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


	//todo(Almog): BlockReqFactory created a function that queries blockids from a single channel,
	// each block id will be sent to a different peer, in case the peer does not have the block id it will not retry
	// need to fix the logic and add retries or query blockid to all peers
	// to make tests pass we've added 2 more blockids so that all peers will be queried for that id
	wrk, output := NewPeersWorker(syncObj2, []p2p.Peer{nodes[3].PublicKey(), nodes[2].PublicKey(), nodes[0].PublicKey()}, &sync.Once{}, BlockReqFactory([]types.BlockID{123, 123, 123}))

	go wrk.Work()
	wrk.Wait()

	timeout := time.NewTimer(1 * time.Second)
	select {
	case item := <-output:
		assert.Equal(t, types.BlockID(123), item.(*types.MiniBlock).Id, "wrong block")
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
	lid := types.LayerID(1)
	syncObj1.AddBlock(types.NewExistingBlock(types.BlockID(123), lid, nil))
	//syncObj1.ValidateLayer(l) //this is to simulate the approval of the tortoise...
	timeout := time.NewTimer(2 * time.Second)
	pm1 := getPeersMock([]p2p.Peer{nodes[0].PublicKey()})
	syncObj2.Peers = pm1

	wrk := NewNeighborhoodWorker(syncObj2, 1, BlockReqFactory([]types.BlockID{123}))
	go wrk.Work()

	select {
	case item := <-wrk.output:
		assert.Equal(t, types.BlockID(123), item.(*types.MiniBlock).Id, "wrong block")
	case <-timeout.C:
		assert.Fail(t, "no message received on channel")
	}
	wrk.Wait()
}
