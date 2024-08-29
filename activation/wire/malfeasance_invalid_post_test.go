package wire

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/signing"
)

func Test_InvalidPostProof(t *testing.T) {
	_, err := signing.NewEdSigner()
	require.NoError(t, err)

	t.Run("valid", func(t *testing.T) {
		// TODO(mafa): implement
	})

	t.Run("post is valid", func(t *testing.T) {
		// TODO(mafa): implement
	})

	t.Run("commitment is invalid", func(t *testing.T) {
		// TODO(mafa): implement
	})

	t.Run("marriage ATX is invalid", func(t *testing.T) {
		// TODO(mafa): implement
	})

	t.Run("invalid signature for commitment", func(t *testing.T) {
		// TODO(mafa): implement
	})

	t.Run("invalid signature for invalid post", func(t *testing.T) {
		// TODO(mafa): implement
	})

	t.Run("invalid signature for marriage ATX", func(t *testing.T) {
		// TODO(mafa): implement
	})
}
