package primitive_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types/primitive"
)

func TestHash20(t *testing.T) {
	require.Equal(t, primitive.CalcHash20([]byte("hello")), primitive.CalcHash32([]byte("hello")).ToHash20())
}
