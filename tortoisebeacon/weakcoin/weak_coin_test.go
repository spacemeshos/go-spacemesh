package weakcoin

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/require"
)

func Test_weakCoin_generateProposal(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	var wc weakCoin
	epoch := types.EpochID(3)
	round := types.RoundID(1)
	expected := "5765616b436f696e03000000000000000100000000000000"

	p, err := wc.generateProposal(epoch, round)
	r.NoError(err)

	r.EqualValues(expected, hex.EncodeToString(p))
}

func Test_weakCoin_proposalExceedsThreshold(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	proposal1, ok := new(big.Int).SetString("0xb150cef2e30473bf3b1d24e253cce39f995c15ebc37bb019d7ab618e873509fc", 0)
	require.True(t, ok)

	proposal2, ok := new(big.Int).SetString("0x01", 0)
	require.True(t, ok)

	threshold, ok := new(big.Int).SetString("0x8000000000000000000000000000000000000000000000000000000000000000", 0)
	require.True(t, ok)

	tt := []struct {
		name      string
		proposal  []byte
		threshold []byte
		expected  bool
	}{
		{
			name:      "Case 1",
			proposal:  proposal1.Bytes(),
			threshold: threshold.Bytes(),
			expected:  true,
		},
		{
			name:      "Case 2",
			proposal:  proposal2.Bytes(),
			threshold: threshold.Bytes(),
			expected:  false,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var wc weakCoin
			wc.threshold = DefaultThreshold()
			got := wc.proposalExceedsThreshold(tc.proposal)
			r.Equal(tc.expected, got)
		})
	}
}

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
