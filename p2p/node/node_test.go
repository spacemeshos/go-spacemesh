package node

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	pu := p2pcrypto.NewRandomPubkey()
	address := "0.0.0.0:1234"

	node := New(pu, address)
	fmt.Print(pu.String())

	assert.Equal(t, node.PublicKey(), pu)
	assert.Equal(t, node.Address(), address)
}

func TestNewNodeFromString(t *testing.T) {
	address := "126.0.0.1:3572"
	pubkey := "DWXX1te9Vr9DNUJVsuQqAhoHgXzXCYvwxfiTHCiyxYF5"
	data := fmt.Sprintf("%v/%v", address, pubkey)

	node, err := NewNodeFromString(data)

	assert.NoError(t, err)
	assert.Equal(t, address, node.Address())
	assert.Equal(t, pubkey, node.PublicKey().String())

	pubkey = "r9gJRWVB9JVPap2HKn"
	data = fmt.Sprintf("%v/%v", address, pubkey)
	node, err = NewNodeFromString(data)
	assert.Error(t, err)
}

func TestStringFromNode(t *testing.T) {
	n := GenerateRandomNodeData()

	str := StringFromNode(n)
	splt := strings.Split(str, "/")

	assert.Equal(t, splt[0], n.address)
	assert.Equal(t, splt[1], n.PublicKey().String())
}
