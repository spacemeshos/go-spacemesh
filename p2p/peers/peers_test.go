package peers

import (
	"bytes"
	"context"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

func getPeers(tb testing.TB) (*Peers, chan p2pcrypto.PublicKey, chan p2pcrypto.PublicKey) {
	tb.Helper()
	peers := New(WithLog(logtest.New(tb)))
	tb.Cleanup(peers.Close)
	// NOTE(dshulyak) don't add buffer. tests correctness depends on a switch when channel blocks on send.
	added := make(chan p2pcrypto.PublicKey)
	expired := make(chan p2pcrypto.PublicKey)
	peers.Start(added, expired)
	return peers, added, expired
}

// sortPeers in place and return a ptr for convenience.
func sortPeers(peers []Peer) []Peer {
	sort.Slice(peers, func(i, j int) bool {
		return bytes.Compare(peers[i].Bytes(), peers[j].Bytes()) == -1
	})
	return peers
}

func TestPeersAddedRemoved(t *testing.T) {
	p, added, expired := getPeers(t)
	expected := make([]Peer, 0, 10)
	for i := 0; i < cap(expected); i++ {
		expected = append(expected, p2pcrypto.NewRandomPubkey())
		added <- expected[i]
	}

	_, err := p.WaitPeers(context.TODO(), len(expected))
	require.NoError(t, err)
	require.Equal(t, sortPeers(expected), sortPeers(p.GetPeers()))

	for _, peer := range expected[5:] {
		expired <- peer
	}
	expected = expected[:5]
	expected = append(expected, p2pcrypto.NewRandomPubkey())
	added <- expected[5]
	_, err = p.WaitPeers(context.TODO(), 6)
	require.NoError(t, err)
	require.Equal(t, sortPeers(expected), sortPeers(p.GetPeers()))
}

func TestPeersWaitBefore(t *testing.T) {
	p, added, _ := getPeers(t)
	watch := make(chan []Peer, 1)
	expected := make([]Peer, 0, 10)
	for i := 0; i < cap(expected); i++ {
		expected = append(expected, p2pcrypto.NewRandomPubkey())
	}
	go func() {
		peers, err := p.WaitPeers(context.TODO(), len(expected))
		assert.NoError(t, err)
		watch <- peers
	}()
	for _, peer := range expected {
		added <- peer
	}
	select {
	case received := <-watch:
		require.Len(t, received, int(p.PeerCount()))
		require.Equal(t, sortPeers(expected), sortPeers(received))
	case <-time.After(10 * time.Millisecond):
		require.FailNow(t, "timed out")
	}
}
