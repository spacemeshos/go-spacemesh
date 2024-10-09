package rangesync

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIncID(t *testing.T) {
	for _, tc := range []struct {
		id, expected KeyBytes
		overflow     bool
	}{
		{
			id:       KeyBytes{0x00, 0x00, 0x00, 0x00},
			expected: KeyBytes{0x00, 0x00, 0x00, 0x01},
			overflow: false,
		},
		{
			id:       KeyBytes{0x00, 0x00, 0x00, 0xff},
			expected: KeyBytes{0x00, 0x00, 0x01, 0x00},
			overflow: false,
		},
		{
			id:       KeyBytes{0xff, 0xff, 0xff, 0xff},
			expected: KeyBytes{0x00, 0x00, 0x00, 0x00},
			overflow: true,
		},
	} {
		id := make(KeyBytes, len(tc.id))
		copy(id, tc.id)
		require.Equal(t, tc.overflow, id.Inc())
		require.Equal(t, tc.expected, id)
	}
}
