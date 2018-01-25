package crypto

import (
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/assert"
	"testing"
)

func TestUUID(t *testing.T) {
	id := UUIDString()
	id1, err := uuid.Parse(id)
	assert.NoErr(t, err, "unexpected error")
	id1Str := id1.String()
	assert.Equal(t, id, id1Str, "expected same uuid")

	id2 := UUID()
	assert.Equal(t, len(id2), 36, "expected 16")

}
