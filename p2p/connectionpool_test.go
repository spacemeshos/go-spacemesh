package p2p

import (
	"testing"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
)

func generatePublicKey() crypto.PublicKey {
	_, pubKey, _ := crypto.GenerateKeyPair()
	return pubKey
}



func TestGetConnectionWithNoConnection(t *testing.T) {
	net := net.NewNetworkMock()
	cPool := NewConnectionPool(net, generatePublicKey())
	remotePub := generatePublicKey()
	cPool.getConnection()
}
