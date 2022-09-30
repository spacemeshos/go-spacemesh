package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

<<<<<<< HEAD
func TestCalcGenesisID(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	tt := []struct {
		name        string
		extraData   string
		genesisTime string
		expected    string
	}{
		{
			name:        "Case 1",
			extraData:   "test",
			genesisTime: "2022-12-25T00:00:00+00:00",
			expected:    "0x9d1bf6fe5807aa341ce3277088282f6809dbaa05",
		},
		{
			name:        "Case 2",
			extraData:   "testqwe",
			genesisTime: "2022-12-25T00:00:00+00:00",
			expected:    "0xc5529b31de900b8ffadd3a134d38bad8b76a89f6",
		},
		{
			name:        "Case 3",
			extraData:   "testqwe",
			genesisTime: "2023-00-00T00:00:00+00:00",
			expected:    "0x301dfce5e5ae9d6c54356fd0e702663462f73dbe",
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := CalcGenesisID(tc.extraData, tc.genesisTime).Hex()
			r.Equal(tc.expected, result)
		})
	}
=======
func TestGenesisID(t *testing.T) {
	t.Run("changes based on cfg vars", func(t *testing.T) {
		cfg1 := GenesisConfig{ExtraData: "one", GenesisTime: "two"}
		cfg2 := GenesisConfig{ExtraData: "one", GenesisTime: "one"}
		require.NotEqual(t, cfg1.GenesisID(), cfg2.GenesisID())
	})
	t.Run("consistent", func(t *testing.T) {
		cfg := GenesisConfig{ExtraData: "one", GenesisTime: "two"}
		require.Equal(t, cfg.GenesisID(), cfg.GenesisID())
	})
	t.Run("stable", func(t *testing.T) {
		cfg := GenesisConfig{ExtraData: "one", GenesisTime: "two"}
		require.Equal(t, "0x91d338938929ec38e320ba558b6bd8538eae972d", cfg.GenesisID().Hex())
	})
>>>>>>> develop
}
