package types

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

var fakeSeq Seq = func(yield func(KeyBytes, error) bool) {
	items := []KeyBytes{
		{1},
		{2},
		{3},
		{4},
	}
	for {
		for _, item := range items {
			if !yield(item, nil) {
				return
			}
		}
	}
}

var fakeErr = errors.New("fake error")

var fakeErrSeq Seq = func(yield func(KeyBytes, error) bool) {
	items := []KeyBytes{
		{1},
		{2},
	}
	for _, item := range items {
		if !yield(item, nil) {
			return
		}
	}
	yield(nil, fakeErr)
}

var fakeErrOnlySeq Seq = func(yield func(KeyBytes, error) bool) {
	yield(nil, fakeErr)
}

func TestFirst(t *testing.T) {
	k, err := fakeSeq.First()
	require.NoError(t, err)
	require.Equal(t, KeyBytes{1}, k)
	k, err = fakeErrSeq.First()
	require.NoError(t, err)
	require.Equal(t, KeyBytes{1}, k)
	k, err = fakeErrOnlySeq.First()
	require.Equal(t, fakeErr, err)
	require.Nil(t, k)
}

func TestGetN(t *testing.T) {
	actual, err := GetN(fakeSeq, 2)
	require.NoError(t, err)
	require.Equal(t, []KeyBytes{{1}, {2}}, actual)
	actual, err = GetN(fakeSeq, 5)
	require.NoError(t, err)
	require.Equal(t, []KeyBytes{{1}, {2}, {3}, {4}, {1}}, actual)
	actual, err = GetN(fakeErrSeq, 2)
	require.NoError(t, err)
	require.Equal(t, []KeyBytes{{1}, {2}}, actual)
	actual, err = GetN(fakeErrSeq, 5)
	require.Equal(t, fakeErr, err)
	require.Nil(t, actual)
	actual, err = GetN(fakeErrOnlySeq, 2)
	require.Equal(t, fakeErr, err)
	require.Nil(t, actual)
}

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
