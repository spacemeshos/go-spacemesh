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
			expected: "0x9c2e4d8fe97d881430de4e754b4205b9c27ce96715231cffc4337340cb110280",
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
