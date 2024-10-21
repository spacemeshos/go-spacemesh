package tortoise

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetBallot(t *testing.T) {
	const n = 2
	s := newSession(t)
	for i := 0; i < n; i++ {
		s.smesher(i).atx(1, new(aopt).height(100).weight(400))
	}

	ref := s.smesher(0).atx(1).ballot(1, new(bopt).
		beacon("a").
		totalEligibilities(s.epochEligibilities()).
		eligibilities(s.layerSize/n))

	secondary := s.smesher(0).atx(1).ballot(2)

	trt := s.tortoise()
	s.runOn(trt)

	require.Equal(t, &BallotData{
		ID:            ref.ID,
		Layer:         ref.Layer,
		ATXID:         ref.AtxID,
		Smesher:       ref.Smesher,
		Beacon:        ref.EpochData.Beacon,
		Eligibilities: ref.EpochData.Eligibilities,
	}, trt.GetBallot(ref.ID))
	require.Equal(t, &BallotData{
		ID:            secondary.ID,
		Layer:         secondary.Layer,
		ATXID:         secondary.AtxID,
		Smesher:       secondary.Smesher,
		Beacon:        ref.EpochData.Beacon,
		Eligibilities: ref.EpochData.Eligibilities,
	}, trt.GetBallot(secondary.ID))
}
