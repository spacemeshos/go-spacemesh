package node

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net"
	"strconv"
	"testing"
)

func TestNew(t *testing.T) {
	pu := p2pcrypto.NewRandomPubkey()
	address := net.IPv6loopback

	port := uint16(1234)

	node := NewNode(pu, address, port, port)

	assert.Equal(t, node.PublicKey(), pu)
	assert.Equal(t, node.IP, address)
}

func TestNewNodeFromString(t *testing.T) {
	address := "126.0.0.1:3572"
	pubkey := "DWXX1te9Vr9DNUJVsuQqAhoHgXzXCYvwxfiTHCiyxYF5"
	data := fmt.Sprintf("spacemesh://%v@%v", pubkey, address)

	node, err := ParseNode(data)

	assert.NoError(t, err)
	ip, port, _ := net.SplitHostPort(address)
	assert.Equal(t, ip, node.IP.String())
	assert.Equal(t, port, strconv.Itoa(int(node.ProtocolPort)))
	assert.Equal(t, pubkey, node.PublicKey().String())

	pubkey = "r9gJRWVB9JVPap2HKn"
	data = fmt.Sprintf("%v/%v", address, pubkey)
	node, err = ParseNode(data)
	require.Nil(t, node)
	require.Error(t, err)
}

func TestStringFromNode(t *testing.T) {
	n := GenerateRandomNodeData()

	str := n.String()

	n2, err := ParseNode(str)

	require.NoError(t, err)

	assert.Equal(t, n2.IP.String(), n.IP.String())
	assert.Equal(t, n2.ID.String(), n.PublicKey().String())
	assert.Equal(t, n2.ProtocolPort, n.ProtocolPort)
	assert.Equal(t, n2.DiscoveryPort, n.DiscoveryPort)
}
