package post

import (
	config "github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestNewTable(t *testing.T) {

	t.Skip() // should be more established before really running.

	config := config.DefaultTestConfig()

	table, err := NewTable(1, "test_post", config.DataDir())
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

	table.deleteAllData()
}
