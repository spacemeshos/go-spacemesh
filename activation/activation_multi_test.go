package activation

import (
	"context"
	"testing"
	"time"

	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

func Test_Builder_Multi_StartSmeshingCoinbase(t *testing.T) {
	tab := newTestBuilder(t)
	coinbase := types.Address{1, 1, 1}

	smeshers := make(map[types.NodeID]*signing.EdSigner, 10)
	for i := 0; i < 10; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		tab.Register(sig)
		smeshers[sig.NodeID()] = sig
	}

	for _, sig := range smeshers {
		tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge).DoAndReturn(
			func(ctx context.Context, _ types.NodeID, _ []byte) (*types.Post, *types.PostInfo, error) {
				<-ctx.Done()
				return nil, nil, ctx.Err()
			})
	}
	tab.mValidator.EXPECT().
		Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		AnyTimes().
		Return(nil)
	tab.mclock.EXPECT().CurrentLayer().Return(types.LayerID(0)).AnyTimes()
	tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(make(chan struct{})).AnyTimes()
	require.NoError(t, tab.StartSmeshing(coinbase))
	require.Equal(t, coinbase, tab.Coinbase())

	// calling StartSmeshing more than once before calling StopSmeshing is an error
	require.ErrorContains(t, tab.StartSmeshing(coinbase), "already started")

	for _, sig := range smeshers {
		tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)
	}
	require.NoError(t, tab.StopSmeshing(true))
}

func Test_Builder_Multi_RestartSmeshing(t *testing.T) {
	getBuilder := func(t *testing.T) *Builder {
		tab := newTestBuilder(t)

		smeshers := make(map[types.NodeID]*signing.EdSigner, 10)
		for i := 0; i < 10; i++ {
			sig, err := signing.NewEdSigner()
			require.NoError(t, err)

			tab.Register(sig)
			smeshers[sig.NodeID()] = sig

			tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge).AnyTimes().DoAndReturn(
				func(ctx context.Context, _ types.NodeID, _ []byte) (*types.Post, *types.PostInfo, error) {
					<-ctx.Done()
					return nil, nil, ctx.Err()
				})
		}

		ch := make(chan struct{})
		close(ch)
		tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(ch).AnyTimes()
		tab.mclock.EXPECT().CurrentLayer().Return(types.LayerID(0)).AnyTimes()
		return tab.Builder
	}

	t.Run("Single threaded", func(t *testing.T) {
		builder := getBuilder(t)
		for i := 0; i < 50; i++ {
			require.NoError(t, builder.StartSmeshing(types.Address{}))
			require.True(t, builder.Smeshing())
			require.NoError(t, builder.StopSmeshing(false))
			require.False(t, builder.Smeshing())
		}
	})

	t.Run("Multi threaded", func(t *testing.T) {
		// Meant to be run with -race to detect races.
		// It cannot check `builder.Smeshing()` as Start/Stop is happening from many goroutines simultaneously.
		// Both Start and Stop can fail as it is not known if builder is smeshing or not.
		builder := getBuilder(t)
		var eg errgroup.Group
		for worker := 0; worker < 10; worker += 1 {
			eg.Go(func() error {
				for i := 0; i < 50; i++ {
					builder.StartSmeshing(types.Address{})
					builder.StopSmeshing(false)
				}
				return nil
			})
		}
		require.NoError(t, eg.Wait())
	})
}

func Test_Builder_Multi_StopSmeshing_Delete(t *testing.T) {
	tab := newTestBuilder(t)

	atx := types.RandomATXID()
	refChallenge := &types.NIPostChallenge{
		PublishEpoch:  postGenesisEpoch + 2,
		CommitmentATX: &atx,
	}

	currLayer := (postGenesisEpoch + 1).FirstLayer()
	tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(make(chan struct{})).AnyTimes()
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()

	smeshers := make(map[types.NodeID]*signing.EdSigner, 10)
	for i := 0; i < 10; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		tab.Register(sig)
		smeshers[sig.NodeID()] = sig

		tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge).DoAndReturn(
			func(ctx context.Context, _ types.NodeID, _ []byte) (*types.Post, *types.PostInfo, error) {
				<-ctx.Done()
				return nil, nil, ctx.Err()
			})

		// add challenge to DB
		require.NoError(t, nipost.AddChallenge(tab.localDb, sig.NodeID(), refChallenge))
	}

	require.NoError(t, tab.StartSmeshing(types.Address{}))
	require.NoError(t, tab.StopSmeshing(false))

	for _, sig := range smeshers {
		challenge, err := nipost.Challenge(tab.localDb, sig.NodeID())
		require.NoError(t, err)
		require.Equal(t, refChallenge, challenge) // challenge still present

		tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)

		tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge).DoAndReturn(
			func(ctx context.Context, _ types.NodeID, _ []byte) (*types.Post, *types.PostInfo, error) {
				<-ctx.Done()
				return nil, nil, ctx.Err()
			})
	}

	require.NoError(t, tab.StartSmeshing(types.Address{}))
	require.NoError(t, tab.StopSmeshing(true))

	for _, sig := range smeshers {
		challenge, err := nipost.Challenge(tab.localDb, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, challenge) // challenge deleted

		tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)

		tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge).DoAndReturn(
			func(ctx context.Context, _ types.NodeID, _ []byte) (*types.Post, *types.PostInfo, error) {
				<-ctx.Done()
				return nil, nil, ctx.Err()
			})
	}

	require.NoError(t, tab.StartSmeshing(types.Address{}))
	require.NoError(t, tab.StopSmeshing(true)) // no-op

	for _, sig := range smeshers {
		challenge, err := nipost.Challenge(tab.localDb, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, challenge) // challenge still deleted
	}
}

func TestRegossip(t *testing.T) {
	layer := types.LayerID(10)

	smeshers := make(map[types.NodeID]*signing.EdSigner, 10)
	for i := 0; i < 10; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		smeshers[sig.NodeID()] = sig
	}

	t.Run("not found", func(t *testing.T) {
		tab := newTestBuilder(t)
		for _, sig := range smeshers {
			tab.Register(sig)
			tab.mclock.EXPECT().CurrentLayer().Return(layer)
			require.NoError(t, tab.Regossip(context.Background(), sig.NodeID()))
		}
	})

	t.Run("success", func(t *testing.T) {
		tab := newTestBuilder(t)
		var refAtx *types.VerifiedActivationTx

		for _, sig := range smeshers {
			tab.Register(sig)

			atx := newActivationTx(t,
				sig, 0, types.EmptyATXID, types.EmptyATXID, nil,
				layer.GetEpoch(), 0, 1, types.Address{}, 1, &types.NIPost{})
			require.NoError(t, atxs.Add(tab.cdb.Database, atx))

			if refAtx == nil {
				refAtx = atx
			}
		}

		blob, err := atxs.GetBlob(tab.cdb.Database, refAtx.ID().Bytes())
		require.NoError(t, err)

		// atx will be regossiped once (by the smesher)
		tab.mclock.EXPECT().CurrentLayer().Return(layer)
		ctx := context.Background()
		tab.mpub.EXPECT().Publish(ctx, pubsub.AtxProtocol, blob)
		require.NoError(t, tab.Regossip(ctx, refAtx.SmesherID))
	})

	t.Run("checkpointed", func(t *testing.T) {
		tab := newTestBuilder(t)
		for _, sig := range smeshers {
			tab.Register(sig)

			require.NoError(t, atxs.AddCheckpointed(tab.cdb.Database,
				&atxs.CheckpointAtx{ID: types.RandomATXID(), Epoch: layer.GetEpoch(), SmesherID: sig.NodeID()}))

			tab.mclock.EXPECT().CurrentLayer().Return(layer)
			require.NoError(t, tab.Regossip(context.Background(), sig.NodeID()))
		}
	})
}

func Test_Builder_Multi_InitialPost(t *testing.T) {
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	smeshers := make(map[types.NodeID]*signing.EdSigner, 10)
	for i := 0; i < 10; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		tab.Register(sig)
		smeshers[sig.NodeID()] = sig
	}

	var eg errgroup.Group
	for _, sig := range smeshers {
		sig := sig
		eg.Go(func() error {
			tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge).Return(
				&types.Post{Indices: make([]byte, 10)},
				&types.PostInfo{
					CommitmentATX: types.RandomATXID(),
					Nonce:         new(types.VRFPostIndex),
				},
				nil,
			)
			tab.mValidator.EXPECT().
				Post(gomock.Any(), sig.NodeID(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				AnyTimes().
				Return(nil)

			require.NoError(t, tab.buildInitialPost(context.Background(), sig.NodeID()))

			// postClient.Proof() should not be called again
			require.NoError(t, tab.buildInitialPost(context.Background(), sig.NodeID()))
			return nil
		})
	}

	eg.Wait()
}
