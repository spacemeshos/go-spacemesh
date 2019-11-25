package sync

import (
	"crypto/sha256"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestNewPeerWorker(t *testing.T) {
	syncs, nodes, _ := SyncMockFactory(4, conf, "TestNewPeerWorker", memoryDB, newMockPoetDb)
	syncObj1 := syncs[0]
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	defer syncObj2.Close()
	bl1 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	err := syncObj1.AddBlock(bl1)
	assert.NoError(t, err)

	wrk, output := NewPeersWorker(syncObj2, []p2p.Peer{nodes[3].PublicKey(), nodes[2].PublicKey(), nodes[0].PublicKey()}, &sync.Once{}, LayerIdsReqFactory(1))

	go wrk.Work()
	wrk.Wait()

	timeout := time.NewTimer(1 * time.Second)
	select {
	case item := <-output:
		assert.Equal(t, bl1.Id(), item.([]types.BlockID)[0], "wrong ids")
	case <-timeout.C:
		assert.Fail(t, "no message received on channel")
	}

}

func TestNewNeighborhoodWorker(t *testing.T) {
	r := require.New(t)
	syncs, nodes, _ := SyncMockFactory(2, conf, "TestSyncer_FetchPoetProofAvailableAndValid_", memoryDB, newMemPoetDb)
	s0 := syncs[0]
	s1 := syncs[1]
	s1.Peers = getPeersMock([]p2p.Peer{nodes[0].PublicKey()})

	proofMessage := makePoetProofMessage(t)

	err := s0.poetDb.ValidateAndStore(&proofMessage)
	r.NoError(err)

	poetProofBytes, err := types.InterfaceToBytes(&proofMessage.PoetProof)
	r.NoError(err)
	ref := sha256.Sum256(poetProofBytes)

	w := NewNeighborhoodWorker(s1, 1, PoetReqFactory(ref[:]))
	go w.work()
	assert.NotNil(t, <-w.output)
	r.NoError(err)
	w.Wait()
}
