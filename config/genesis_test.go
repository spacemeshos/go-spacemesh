package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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

			result := CalcGenesisID([]byte(tc.extraData), tc.genesisTime).Hex()
			r.Equal(tc.expected, result)
		})
	}
}

func TestCalcGoldenATX(t *testing.T) {
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
			expected:    "0x9d1bf6fe5807aa341ce3277088282f6809dbaa05000000000000000000000000",
		},
		{
			name:        "Case 2",
			extraData:   "testqwe",
			genesisTime: "2022-12-25T00:00:00+00:00",
			expected:    "0xc5529b31de900b8ffadd3a134d38bad8b76a89f6000000000000000000000000",
		},
		{
			name:        "Case 3",
			extraData:   "testqwe",
			genesisTime: "2023-00-00T00:00:00+00:00",
			expected:    "0x301dfce5e5ae9d6c54356fd0e702663462f73dbe000000000000000000000000",
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := CalcGoldenATX([]byte(tc.extraData), tc.genesisTime).Hex()
			r.Equal(tc.expected, result)
		})
	}
}
