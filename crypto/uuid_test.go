package crypto

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestUUID(t *testing.T) {
	id := UUIDString()
	id1, err := uuid.Parse(id)
	assert.NoError(t, err, "unexpected error")
	id1Str := id1.String()
	assert.Equal(t, id, id1Str, "expected same uuid")

	id2 := NewUUID()
	assert.Equal(t, len(id2), 16, "expected 16")

}
