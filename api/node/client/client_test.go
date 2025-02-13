package client

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestNewNodeServiceClient(t *testing.T) {
	t.Run("missing server address", func(t *testing.T) {
		_, err := NewNodeServiceClient("", zaptest.NewLogger(t), &Config{})
		require.ErrorContains(t, err, "missing node-service server address")
	})
}
