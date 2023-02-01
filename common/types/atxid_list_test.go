package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestATXIDList_Hash(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	tt := []struct {
		name     string
		atxList  ATXIDList
		expected string
	}{
		{
			name:     "Case 1",
			atxList:  ATXIDList{ATXID(CalcHash32([]byte("1")))},
			expected: "0x3c462dd7363d6e6fb7d5085679d2ee7de30f2d939722882be0816387983051f6",
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := tc.atxList.Hash().String()
			r.Equal(tc.expected, result)
		})
	}
}
