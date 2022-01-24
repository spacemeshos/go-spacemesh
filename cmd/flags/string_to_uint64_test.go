package flags

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStringToUint64Value(t *testing.T) {
	for _, tc := range []struct {
		desc     string
		input    []string
		err      string
		expected map[string]uint64
	}{
		{
			desc:  "Joined",
			input: []string{"1=1,2=2"},
			expected: map[string]uint64{
				"1": 1,
				"2": 2,
			},
		},
		{
			desc:  "Separate",
			input: []string{"1=1", "2=2"},
			expected: map[string]uint64{
				"1": 1,
				"2": 2,
			},
		},
		{
			desc:  "InvalidSeparator",
			input: []string{"1->1"},
			err:   "1->1 must be formatted as key=value",
		},
		{
			desc:  "NoSeparator",
			input: []string{"1"},
			err:   "1 must be formatted as key=value",
		},
		{
			desc:  "InvalidInteger",
			input: []string{"1=abc"},
			err:   "strconv.ParseUint: parsing \"abc\": invalid syntax",
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			value := map[string]uint64{}
			parser := NewStringToUint64Value(value)
			full := ""
			for _, arg := range tc.input {
				full += arg + ","
				err := parser.Set(arg)
				if len(tc.err) > 0 {
					require.NotNil(t, err)
					require.Contains(t, err.Error(), tc.err)
				} else {
					require.NoError(t, err)
				}
			}
			full = full[:len(full)-1]
			if len(tc.err) == 0 {
				require.Equal(t, tc.expected, value)
				require.Equal(t, tc.expected, CastStringToMapStringUint64(full))
			} else {
				require.Nil(t, CastStringToMapStringUint64(full))
			}
		})
	}
}
