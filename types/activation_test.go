package types

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestActivationTx_CalcAndSetId(t *testing.T) {
	atx := NewActivationTx(NodeId{"abcd", []byte{1, 2, 3}}, address.Address{}, 1, AtxId{}, 1, 0, AtxId{}, 5, nil, &NIPST{})
	fmt.Println(atx.id)
	atx.CalcAndSetId()
	prevId := atx.id
	fmt.Println(atx.id)
	atx.CalcAndSetId()
	assert.Equal(t, prevId, atx.id)
	fmt.Println(atx.id)
}
