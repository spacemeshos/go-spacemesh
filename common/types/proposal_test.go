package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProposalIDSize(t *testing.T) {
	var id ProposalID
	assert.Len(t, id.Bytes(), ProposalIDSize)
}
