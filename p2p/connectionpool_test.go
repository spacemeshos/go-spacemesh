package p2p

import (
	"testing"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/stretchr/testify/assert"
)

func generatePublicKey() crypto.PublicKey {
	_, pubKey, _ := crypto.GenerateKeyPair()
	return pubKey
}

func TestGetConnectionWithNoConnection(t *testing.T) {
	net := net.NewNetworkMock()
	cPool := NewConnectionPool(net, generatePublicKey())
	remotePub := generatePublicKey()
	_, err := cPool.getConnection("1.1.1.1", remotePub)
	assert.Nil(t, err, "Failed to get new Connection")
}
