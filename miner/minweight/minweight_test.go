package minweight

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestSelect(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		require.EqualValues(t, 0, Select(0, nil))
	})
	t.Run("sorted", func(t *testing.T) {
		require.EqualValues(t, 10, Select(5, []types.EpochMinimalActiveWeight{
			{Epoch: 0, Weight: 1},
			{Epoch: 4, Weight: 5},
			{Epoch: 5, Weight: 10},
			{Epoch: 7, Weight: 11},
		}))
	})
	t.Run("in-between epochs", func(t *testing.T) {
		require.EqualValues(t, 10, Select(6, []types.EpochMinimalActiveWeight{
			{Epoch: 0, Weight: 1},
			{Epoch: 4, Weight: 5},
			{Epoch: 5, Weight: 10},
			{Epoch: 7, Weight: 11},
		}))
	})
	t.Run("after all", func(t *testing.T) {
		require.EqualValues(t, 11, Select(10, []types.EpochMinimalActiveWeight{
			{Epoch: 0, Weight: 1},
			{Epoch: 4, Weight: 5},
			{Epoch: 5, Weight: 10},
			{Epoch: 7, Weight: 11},
		}))
	})
	t.Run("not sorted panic", func(t *testing.T) {
		require.Panics(t, func() {
			Select(5, []types.EpochMinimalActiveWeight{
				{Epoch: 0, Weight: 1},
				{Epoch: 5, Weight: 10},
				{Epoch: 4, Weight: 5},
				{Epoch: 7, Weight: 11},
			})
		})
	})
}
