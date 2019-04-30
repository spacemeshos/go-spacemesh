package dht

import (
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/stretchr/testify/require"
	"testing"
)

func Test_discNodeFromNode(t *testing.T) {
	ip := "225.31.23.51"
	ipport := ip + ":7513"
	nr := node.New(p2pcrypto.NewRandomPubkey(), ipport)
	d := discNodeFromNode(nr, ipport)
	require.Equal(t, d.parsedIP.String(), ip)
}
