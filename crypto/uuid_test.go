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

	id2 := UUID()
	assert.Equal(t, len(id2), 36, "expected 16")

}
