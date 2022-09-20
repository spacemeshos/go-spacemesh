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
			expected:    "0x835148077865c83b4f7c805abd3d4b0ac1bff5cd",
		},
		{
			name:        "Case 2",
			extraData:   "testqwe",
			genesisTime: "2022-12-25T00:00:00+00:00",
			expected:    "0xf3c79279e2483defccb9be18ea4c9a428d257499",
		},
		{
			name:        "Case 3",
			extraData:   "testqwe",
			genesisTime: "2023-00-00T00:00:00+00:00",
			expected:    "0x1482190e9ee33f45733ee81407ba3a6589ee420a",
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := CalcGenesisId([]byte(tc.extraData), tc.genesisTime).Hex()
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
			expected:    "0x835148077865c83b4f7c805abd3d4b0ac1bff5cd000000000000000000000000",
		},
		{
			name:        "Case 2",
			extraData:   "testqwe",
			genesisTime: "2022-12-25T00:00:00+00:00",
			expected:    "0xf3c79279e2483defccb9be18ea4c9a428d257499000000000000000000000000",
		},
		{
			name:        "Case 3",
			extraData:   "testqwe",
			genesisTime: "2023-00-00T00:00:00+00:00",
			expected:    "0x1482190e9ee33f45733ee81407ba3a6589ee420a000000000000000000000000",
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
