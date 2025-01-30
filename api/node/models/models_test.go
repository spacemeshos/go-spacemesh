package models

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func Test_parseAtx(t *testing.T) {
	atxid := types.ATXID{1, 2, 3, 4, 5}
	nodeID := types.NodeID{6, 7, 8, 9, 0}

	validAtx := ActivationTx{
		ID:           atxid[:],
		NumUnits:     8,
		PublishEpoch: 9,
		SmesherID:    nodeID[:],
		TickCount:    5,
		Weight:       4,
	}
	t.Run("ok", func(t *testing.T) {
		sequence := uint64(7)
		atx := validAtx
		atx.Sequence = &sequence

		result, err := ParseATX(&atx)
		require.NoError(t, err)

		expected := types.ActivationTx{
			PublishEpoch: types.EpochID(atx.PublishEpoch),
			Sequence:     *atx.Sequence,
			NumUnits:     atx.NumUnits,
			TickCount:    atx.TickCount,
			SmesherID:    nodeID,
			Weight:       atx.Weight,
		}
		expected.SetID(atxid)
		require.Equal(t, &expected, result)
	})
	t.Run("ok, empty sequence", func(t *testing.T) {
		result, err := ParseATX(&validAtx)
		require.NoError(t, err)

		expected := types.ActivationTx{
			PublishEpoch: types.EpochID(validAtx.PublishEpoch),
			Sequence:     0,
			NumUnits:     validAtx.NumUnits,
			TickCount:    validAtx.TickCount,
			SmesherID:    nodeID,
			Weight:       validAtx.Weight,
		}
		expected.SetID(atxid)
		require.Equal(t, &expected, result)
	})
	t.Run("invalid nodeID length", func(t *testing.T) {
		atx := validAtx
		atx.SmesherID = []byte("CAFE")
		_, err := ParseATX(&atx)
		require.Error(t, err)
	})
	t.Run("invalid atx ID length", func(t *testing.T) {
		atx := validAtx
		atx.ID = []byte("CAFE")
		_, err := ParseATX(&atx)
		require.Error(t, err)
	})
}
