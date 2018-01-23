package test

import (
	"testing"

	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/merkle"
)

func TestDelete(t *testing.T) {
	mt, _ := merkle.NewEmptyTree("userdata", "treedata")

	mt.Put([]byte("test_key"), []byte("test_value"))
	mt.Delete([]byte("test_key"))
	value, _, err := mt.Get([]byte("test_key"))

	assert.NoErr(t, err, "There should be no error")
	assert.Equal(t, 0, len(value), "Value lenght should be 0")
}
