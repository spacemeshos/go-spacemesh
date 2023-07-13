package config

import (
	"testing"

	"github.com/spacemeshos/economics/constants"
	"github.com/stretchr/testify/require"
)

func TestVaulted(t *testing.T) {
	sum := uint64(0)
	for _, balance := range MainnetAccounts() {
		sum += balance
	}
	require.Equal(t, constants.TotalVaulted, int(sum))
}
