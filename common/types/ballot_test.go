package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBallotIDSize(t *testing.T) {
	var id BallotID
	assert.Len(t, id.Bytes(), BallotIDSize)
}
