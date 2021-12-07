package tortoise

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/tortoise/sim"
)

func TestRerunEvictConcurrent(t *testing.T) {
	ctx := context.Background()
	const size = 30
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	tortoise := tortoiseFromSimState(s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))

	for i := 0; i < int(cfg.WindowSize); i++ {
		tortoise.HandleIncomingLayer(ctx, s.Next())
	}
	require.NoError(t, tortoise.Persist(ctx))
	require.NoError(t, tortoise.rerun(ctx))
	for i := 0; i < int(cfg.WindowSize); i++ {
		// have to use private trtl to simulate concurrent layers without replacing tortoise instance
		lid := s.Next()
		tortoise.org.Iterate(ctx, lid, func(lid types.LayerID) {
			tortoise.trtl.HandleIncomingLayer(ctx, lid)
		})
	}
	require.NoError(t, tortoise.Persist(ctx))
	last := s.Next()
	tortoise.HandleIncomingLayer(ctx, last)
	require.NoError(t, tortoise.Persist(ctx))

	// st := state{log: logtest.New(t), db: tortoise.trtl.db}
	// require.NoError(t, st.Recover())
	// require.Equal(t, last.Sub(1), st.Verified)
	// for lid := range st.BallotOpinionsByLayer {
	// 	require.True(t, lid.After(st.LastEvicted))
	// }
}
