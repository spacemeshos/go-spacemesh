package tortoise

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/tortoise/sim"
)

func TestRerunAndRevert(t *testing.T) {
	r := require.New(t)
	mdb := getInMemMesh(t)
	atxdb := getAtxDB()
	alg := defaultAlgorithm(t, mdb)
	alg.trtl.atxdb = atxdb
	mdb.InputVectorBackupFunc = mdb.LayerBlockIds

	// process a couple of layers
	l0ID := types.GetEffectiveGenesis()
	l1ID := l0ID.Add(1)
	l2ID := l1ID.Add(1)
	makeLayer(t, l1ID, alg.trtl, defaultTestLayerSize, atxdb, mdb, mdb.LayerBlockIds)
	l1IDs, err := mdb.LayerBlockIds(l1ID)
	r.NoError(err)
	block1ID := l1IDs[0]
	r.NoError(mdb.SaveLayerInputVectorByID(context.TODO(), l1ID, l1IDs))
	alg.HandleIncomingLayer(context.TODO(), l1ID)
	makeLayer(t, l2ID, alg.trtl, defaultTestLayerSize, atxdb, mdb, mdb.LayerBlockIds)
	l2IDs, err := mdb.LayerBlockIds(l2ID)
	r.NoError(err)
	r.NoError(mdb.SaveLayerInputVectorByID(context.TODO(), l2ID, l2IDs))
	oldVerified, newVerified, reverted := alg.HandleIncomingLayer(context.TODO(), l2ID)
	r.Equal(int(l0ID.Uint32()), int(oldVerified.Uint32()))
	r.Equal(int(l1ID.Uint32()), int(newVerified.Uint32()))
	r.False(reverted)
	r.Equal(int(l1ID.Uint32()), int(alg.trtl.Verified.Uint32()))
	isValid, err := mdb.ContextualValidity(block1ID)
	r.NoError(err)
	r.True(isValid)

	// now change some state so that the opinion on layer/block validity changes

	// local opinion
	mdb.InputVectorBackupFunc = func(types.LayerID) ([]types.BlockID, error) {
		// empty slice means vote against all
		return []types.BlockID{}, nil
	}

	// global opinion: add a bunch of blocks that vote against l1 blocks
	// for these blocks to be good, they must have an old base block, since they'll get exception votes on
	// more recent blocks
	baseBlockFn := func(ctx context.Context) (types.BlockID, [][]types.BlockID, error) {
		return mesh.GenesisBlock().ID(), [][]types.BlockID{nil, nil, nil}, nil
	}
	l2 := createTurtleLayer(t, l2ID, mdb, atxdb, baseBlockFn, mdb.LayerBlockIds, defaultTestLayerSize*3)
	for _, block := range l2.Blocks() {
		r.NoError(mdb.AddBlock(block))
	}

	// force a rerun and make sure there was a reversion
	require.NoError(t, alg.rerun(context.TODO()))
	oldVerified, newVerified, reverted = alg.HandleIncomingLayer(context.TODO(), l2ID)
	r.Equal(int(l0ID.Uint32()), int(oldVerified.Uint32()))
	r.Equal(int(l1ID.Uint32()), int(newVerified.Uint32()))
	r.True(reverted)
	r.Equal(int(l1ID.Uint32()), int(alg.trtl.Verified.Uint32()))
	isValid, err = mdb.ContextualValidity(block1ID)
	r.NoError(err)
	r.False(isValid)
}

func TestRerunEvictConcurrent(t *testing.T) {
	ctx := context.Background()
	const size = 30
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	tortoise := tortoiseFromSimState(s.State, WithLogger(logtest.New(t)), WithConfig(cfg))

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

	st := state{log: logtest.New(t), db: tortoise.trtl.db}
	require.NoError(t, st.Recover())
	require.Equal(t, last.Sub(1), st.Verified)
	for lid := range st.BallotOpinionsByLayer {
		require.True(t, lid.After(st.LastEvicted))
	}
}
