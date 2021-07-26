package weakcoin

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

/*
func Test_weakCoin_generateProposal(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	logger := log.NewDefault("WeakCoin")
	wcg := NewWeakCoin(DefaultPrefix, DefaultThreshold, nil, logger)
	epoch := types.EpochID(3)
	round := types.RoundID(1)
	expected := "0xb150cef2e30473bf3b1d24e253cce39f995c15ebc37bb019d7ab618e873509fc"

	p, err := wcg.(*weakCoin).generateProposal(epoch, round)
	r.NoError(err)

	r.EqualValues(expected, p.String())
}

func Test_weakCoin_proposalExceedsThreshold(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	logger := log.NewDefault("WeakCoin")

	tt := []struct {
		name      string
		proposal  types.Hash32
		threshold types.Hash32
		expected  bool
	}{
		{
			name:      "Case 1",
			proposal:  types.HexToHash32("0xb150cef2e30473bf3b1d24e253cce39f995c15ebc37bb019d7ab618e873509fc"),
			threshold: types.HexToHash32("0x80" + strings.Repeat("00", 31)),
			expected:  true,
		},
		{
			name:      "Case 2",
			proposal:  types.HexToHash32("0x01"),
			threshold: types.HexToHash32("0x80" + strings.Repeat("00", 31)),
			expected:  false,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			wcg := NewWeakCoin(DefaultPrefix, DefaultThreshold, nil, logger)
			got := wcg.(*weakCoin).proposalExceedsThreshold(tc.proposal)
			r.Equal(tc.expected, got)
		})
	}
}

*/

func Test_weakCoin_coinBitToBool(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	tt := []struct {
		name          string
		coinBit       *big.Int
		expectedValue bool
		expectedPanic bool
	}{
		{
			name:          "True",
			coinBit:       big.NewInt(1),
			expectedValue: true,
		},
		{
			name:          "False",
			coinBit:       big.NewInt(0),
			expectedValue: false,
		},
		{
			name:          "Panic",
			coinBit:       big.NewInt(3),
			expectedPanic: true,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.expectedPanic {
				f := func() {
					coinBitToBool(tc.coinBit)
				}
				r.Panics(f)
			} else {
				r.Equal(tc.expectedValue, coinBitToBool(tc.coinBit))
			}
		})
	}
}
