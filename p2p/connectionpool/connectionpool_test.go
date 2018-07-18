package connectionpool

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/stretchr/testify/assert"
	"testing"
)

func generatePublicKey() crypto.PublicKey {
	_, pubKey, _ := crypto.GenerateKeyPair()
	return pubKey
}

func TestGetConnectionWithNoConnection(t *testing.T) {
	net := net.NewNetworkMock()
	cPool := NewConnectionPool(net, generatePublicKey())
	remotePub := generatePublicKey()
		conn, err := cPool.GetConnection("1.1.1.1", remotePub)
	assert.Nil(t, err, "Failed to get new Connection")
	assert.Equal(t, remotePub.String(), conn.RemotePublicKey().String(), "mismatch in public key")
}
