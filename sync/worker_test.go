package sync

import (
	"crypto/sha256"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	p2ppeers "github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/rand"
)

func TestNewPeerWorker(t *testing.T) {
	syncs, nodes, _ := SyncMockFactory(4, conf, "TestNewPeerWorker", memoryDB, newMockPoetDb)
	syncObj1 := syncs[0]
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	defer syncObj2.Close()
	bl1 := types.NewExistingBlock(types.GetEffectiveGenesis()+1, []byte(rand.String(8)))
	err := syncObj1.AddBlock(bl1)
	assert.NoError(t, err)

	wrk := newPeersWorker(syncObj2, []p2ppeers.Peer{nodes[3].PublicKey(), nodes[2].PublicKey(), nodes[0].PublicKey()}, &sync.Once{}, layerIdsReqFactory(types.GetEffectiveGenesis()+1))

	go wrk.Work()

	timeout := time.NewTimer(1 * time.Second)
	select {
	case item := <-wrk.output:
		assert.Equal(t, bl1.ID(), item.([]types.BlockID)[0], "wrong ids e: %v a: %v", bl1.ID().String(), item.([]types.BlockID)[0].String())
	case <-timeout.C:
		assert.Fail(t, "no message received on channel")
	}

}

func TestNewNeighborhoodWorker(t *testing.T) {
	r := require.New(t)
	syncs, nodes, _ := SyncMockFactory(2, conf, "TestSyncer_FetchPoetProofAvailableAndValid_", memoryDB, newMemPoetDb)
	s0 := syncs[0]
	s1 := syncs[1]
	s1.peers = getPeersMock([]p2ppeers.Peer{nodes[0].PublicKey()})

	proofMessage := makePoetProofMessage(t)

	err := s0.poetDb.ValidateAndStore(&proofMessage)
	r.NoError(err)

	poetProofBytes, err := types.InterfaceToBytes(&proofMessage.PoetProof)
	r.NoError(err)
	ref := sha256.Sum256(poetProofBytes)

	w := newNeighborhoodWorker(s1, 1, poetReqFactory(ref[:]))
	go w.Work()
	assert.NotNil(t, <-w.output)
	r.NoError(err)
}

var longConf = Configuration{1000, 1, 300, 5 * time.Minute, 1 * time.Second, 10 * time.Hour, 100, 5, false}

func TestNeighborhoodWorkerClose(t *testing.T) {
	r := require.New(t)
	syncs, nodes, _ := SyncMockFactory(2, longConf, "TestSyncer_FetchPoetProofAvailableAndValid_", memoryDB, newMemPoetDb)
	syncs[0].Close()
	s1 := syncs[1]
	s1.peers = getPeersMock([]p2ppeers.Peer{nodes[0].PublicKey()})

	proofMessage := makePoetProofMessage(t)
	poetProofBytes, err := types.InterfaceToBytes(&proofMessage.PoetProof)
	r.NoError(err)
	ref := sha256.Sum256(poetProofBytes)

	w := newNeighborhoodWorker(s1, 1, poetReqFactory(ref[:]))
	go w.Work()
	go func() {
		time.Sleep(time.Second)
		s1.Close()
	}()
	<-w.output
	log.Info("closed")
}

func TestPeerWorkerClose(t *testing.T) {
	syncs, nodes, _ := SyncMockFactory(4, longConf, "TestNewPeerWorker", memoryDB, newMockPoetDb)
	syncObj1 := syncs[0]
	syncObj1.Close()
	syncObj2 := syncs[1]
	wrk := newPeersWorker(syncObj2, []p2ppeers.Peer{nodes[3].PublicKey(), nodes[2].PublicKey(), nodes[0].PublicKey()}, &sync.Once{}, layerIdsReqFactory(1))
	go wrk.Work()
	go func() {
		time.Sleep(time.Second)
		syncObj2.Close()
	}()
	<-wrk.output
	log.Info("closed")
}
