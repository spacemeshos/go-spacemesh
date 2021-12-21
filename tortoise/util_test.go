package tortoise

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMode(t *testing.T) {
	var m mode
	m = m.toggleRerun()
	require.True(t, m.isRerun())
	require.True(t, m.isVerifying())
	require.False(t, m.isFull())
	m = m.toggleMode()
	require.True(t, m.isRerun())
	require.True(t, m.isFull())
}
