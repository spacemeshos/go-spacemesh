package api

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestNewPeerCounter(t *testing.T) {
	r := require.New(t)

	conn, disc := make(chan p2pcrypto.PublicKey), make(chan p2pcrypto.PublicKey)
	peerCounter := NewPeerCounter(conn, disc)

	r.Zero(peerCounter.PeerCount())
	r.NoError(sendOnChan(conn))
	r.Equal(uint64(1), peerCounter.PeerCount())
	r.NoError(sendOnChan(conn))
	r.Equal(uint64(2), peerCounter.PeerCount())
	r.NoError(sendOnChan(disc))
	r.Equal(uint64(1), peerCounter.PeerCount())
	r.NoError(sendOnChan(disc))
	r.Zero(peerCounter.PeerCount())
	r.NoError(sendOnChan(disc))
	// error should be logged: "peer counter got more disconnections than connections"
	r.Zero(peerCounter.PeerCount())
	r.NoError(sendOnChan(conn))
	r.Equal(uint64(1), peerCounter.PeerCount())
	r.NoError(sendOnChan(conn))
	r.Equal(uint64(2), peerCounter.PeerCount())

	close(conn)
	time.Sleep(1 * time.Millisecond)
	// the loop should terminate and the counter value should now remain constant
	r.EqualError(sendOnChan(disc), "timeout")
	r.EqualError(sendOnChan(disc), "timeout")
	r.Equal(uint64(2), peerCounter.PeerCount())
}

func sendOnChan(ch chan p2pcrypto.PublicKey) error {
	select {
	case <-time.After(10 * time.Millisecond):
		return fmt.Errorf("timeout")
	case ch <- p2pcrypto.PublicKeyFromArray([32]byte{}):
		time.Sleep(1 * time.Millisecond)
	}
	return nil
}
