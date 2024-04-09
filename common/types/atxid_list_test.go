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
			expected: "3c462dd736",
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := tc.atxList.Hash().String()
			r.Equal(tc.expected, result)
		})
	}
}
