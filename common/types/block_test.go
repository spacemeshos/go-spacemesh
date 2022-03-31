package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBlock_IDSize(t *testing.T) {
	var id BlockID
	assert.Len(t, id.Bytes(), BlockIDSize)
}
