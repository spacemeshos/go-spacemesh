package post

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestPingProtocol(t *testing.T) {

	node, _ := p2p.GenerateTestNode(t)

	dir, err := node.EnsureNodeDataDirectory()
	assert.NoError(t, err, "expected node data dir")

	table, err := NewTable(1, node.String(), dir)
	assert.NoError(t, err, "expected no error")

	err = table.deleteAllData()
	assert.NoError(t, err, "failed to delete table data")

	data := make([]byte, 1)

	for i := 0; i < 2*65536; i++ {
		err := crypto.GetRandomBytesToBuffer(1, data)
		assert.NoError(t, err, "failed to generate random data")
		table.write(data)
	}

	// sync all writes to disk
	table.sync()

	//table.deleteAllData()
	node.Shutdown()

}
