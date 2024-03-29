package primitive_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types/primitive"
)

func TestHash20(t *testing.T) {
	require.Equal(t, primitive.CalcHash20([]byte("hello")), primitive.CalcHash32([]byte("hello")).ToHash20())
}

func TestHash30ToBytes(t *testing.T) {
	b := []byte("0123456789abcdef_hello_world_how_are_you") // 40 bytes
	var h primitive.Hash32
	h.SetBytes(b)
	require.Equal(t, []byte("89abcdef_hello_world_how_are_you"), h.Bytes())
}

func TestHash20ToBytes(t *testing.T) {
	b := []byte("0123456789abcdef_hello_world_how_are_you") // 40 bytes
	var h primitive.Hash20
	h.SetBytes(b)
	require.Equal(t, []byte("lo_world_how_are_you"), h.Bytes())
}
