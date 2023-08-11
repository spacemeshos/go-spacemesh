package util

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestNumEligible(t *testing.T) {
	types.SetLegacyLayers(10)
	n, err := GetLegacyNumEligible(11, 100, 0, 1000, 4032, 50)
	require.NoError(t, err)
	require.Equal(t, 20160, int(n))
	n, err = GetLegacyNumEligible(types.LayerID(types.GetLegacyLayer()), 100, 0, 1000, 4032, 50)
	require.NoError(t, err)
	require.Equal(t, 1, int(n))
	n, err = GetLegacyNumEligible(0, 100, 0, 1000, 4032, 50)
	require.NoError(t, err)
	require.Equal(t, 1, int(n))
}
