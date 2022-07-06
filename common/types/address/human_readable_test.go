package address_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types/address"
)

func TestAddress_HRP(t *testing.T) {
	t.Parallel()
	table := []struct {
		name        string
		networkID   address.Network
		networkName string
		networkHRP  string
	}{
		{
			name:        "mainnet",
			networkName: address.MainnetName,
			networkID:   address.MainnetID,
			networkHRP:  address.MainnetHRP,
		},
		{
			name:        "testnet",
			networkName: address.TestnetName,
			networkID:   address.TestnetID,
			networkHRP:  address.TestnetHRP,
		},
	}
	for _, tc := range table {
		t.Run(tc.name, func(t *testing.T) {
			addr, err := address.GenerateAddress(tc.networkID, generatePublicKey(t))
			require.NoError(t, err)
			name, err := addr.GetNetwork()
			require.NoError(t, err)
			require.Equal(t, tc.networkName, name)
			hrp, err := addr.GetHRPNetwork()
			require.NoError(t, err)
			require.Equal(t, tc.networkHRP, hrp)
		})
	}
}
