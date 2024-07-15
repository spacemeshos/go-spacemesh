package activation

import (
	"context"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

func Test_Builder_Multi_StartSmeshingCoinbase(t *testing.T) {
	tab := newTestBuilder(t, 5)
	coinbase := types.Address{1, 1, 1}

	for _, sig := range tab.signers {
		tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge, nil).DoAndReturn(
			func(ctx context.Context, _ types.NodeID, _ []byte, _ *types.NIPostChallenge,
			) (*types.Post, *types.PostInfo, error) {
				<-ctx.Done()
				return nil, nil, ctx.Err()
			})
	}
	tab.mclock.EXPECT().CurrentLayer().Return(types.LayerID(0)).AnyTimes()
	tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(make(chan struct{})).AnyTimes()
	require.NoError(t, tab.StartSmeshing(coinbase))
	require.Equal(t, coinbase, tab.Coinbase())

	// calling StartSmeshing more than once before calling StopSmeshing is an error
	require.ErrorContains(t, tab.StartSmeshing(coinbase), "already started")

	for _, sig := range tab.signers {
		tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)
	}
	require.NoError(t, tab.StopSmeshing(true))
}

func Test_Builder_Multi_RestartSmeshing(t *testing.T) {
	getBuilder := func(t *testing.T) *Builder {
		tab := newTestBuilder(t, 5)

		for _, sig := range tab.signers {
			tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge, nil).AnyTimes().DoAndReturn(
				func(ctx context.Context, _ types.NodeID, _ []byte, _ *types.NIPostChallenge,
				) (*types.Post, *types.PostInfo, error) {
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
		for range 50 {
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
				for range 50 {
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
	numIds := 5
	tab := newTestBuilder(t, numIds)

	atx := types.RandomATXID()
	refChallenge := &types.NIPostChallenge{
		PublishEpoch:  postGenesisEpoch + 2,
		CommitmentATX: &atx,
	}

	currLayer := (postGenesisEpoch + 1).FirstLayer()
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).Times(numIds)
	tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(make(chan struct{})).Times(numIds)

	for _, sig := range tab.signers {
		tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge, nil).DoAndReturn(
			func(ctx context.Context, _ types.NodeID, _ []byte, _ *types.NIPostChallenge,
			) (*types.Post, *types.PostInfo, error) {
				<-ctx.Done()
				return nil, nil, ctx.Err()
			})

		// add challenge to DB
		require.NoError(t, nipost.AddChallenge(tab.localDb, sig.NodeID(), refChallenge))
	}

	require.NoError(t, tab.StartSmeshing(types.Address{}))
	require.NoError(t, tab.StopSmeshing(false))
	require.True(t, tab.mctrl.Satisfied(), "failed to assert all mocks were called the expected number of times")

	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).Times(numIds)
	tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(make(chan struct{})).Times(numIds)

	for _, sig := range tab.signers {
		challenge, err := nipost.Challenge(tab.localDb, sig.NodeID())
		require.NoError(t, err)
		require.Equal(t, refChallenge, challenge) // challenge still present

		tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)
		tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge, nil).DoAndReturn(
			func(ctx context.Context, _ types.NodeID, _ []byte, _ *types.NIPostChallenge,
			) (*types.Post, *types.PostInfo, error) {
				<-ctx.Done()
				return nil, nil, ctx.Err()
			})
	}

	require.NoError(t, tab.StartSmeshing(types.Address{}))
	require.NoError(t, tab.StopSmeshing(true))
	require.True(t, tab.mctrl.Satisfied(), "failed to assert all mocks were called the expected number of times")

	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).Times(numIds)
	tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(make(chan struct{})).Times(numIds)

	for _, sig := range tab.signers {
		challenge, err := nipost.Challenge(tab.localDb, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, challenge) // challenge deleted

		tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)
		tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge, nil).DoAndReturn(
			func(ctx context.Context, _ types.NodeID, _ []byte, _ *types.NIPostChallenge,
			) (*types.Post, *types.PostInfo, error) {
				<-ctx.Done()
				return nil, nil, ctx.Err()
			})
	}

	require.NoError(t, tab.StartSmeshing(types.Address{}))
	require.NoError(t, tab.StopSmeshing(true)) // no-op
	require.True(t, tab.mctrl.Satisfied(), "failed to assert all mocks were called the expected number of times")

	for _, sig := range tab.signers {
		challenge, err := nipost.Challenge(tab.localDb, sig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, challenge) // challenge still deleted
	}
}

func TestRegossip(t *testing.T) {
	layer := types.LayerID(10)

	t.Run("not found", func(t *testing.T) {
		tab := newTestBuilder(t, 5)
		for _, sig := range tab.signers {
			tab.mclock.EXPECT().CurrentLayer().Return(layer)
			require.NoError(t, tab.Regossip(context.Background(), sig.NodeID()))
		}
	})

	t.Run("success", func(t *testing.T) {
		goldenATXID := types.RandomATXID()
		tab := newTestBuilder(t, 5)
		var refAtx *types.ActivationTx

		for _, sig := range tab.signers {
			atx := newInitialATXv1(t, goldenATXID)
			atx.PublishEpoch = layer.GetEpoch()
			atx.Sign(sig)
			vAtx := toAtx(t, atx)
			require.NoError(t, atxs.Add(tab.db, vAtx, atx.Blob()))

			if refAtx == nil {
				refAtx = vAtx
			}
		}

		var blob sql.Blob
		ver, err := atxs.LoadBlob(context.Background(), tab.db, refAtx.ID().Bytes(), &blob)
		require.NoError(t, err)
		require.Equal(t, types.AtxV1, ver)

		// atx will be regossiped once (by the smesher)
		tab.mclock.EXPECT().CurrentLayer().Return(layer)
		ctx := context.Background()
		tab.mpub.EXPECT().Publish(ctx, pubsub.AtxProtocol, blob.Bytes)
		require.NoError(t, tab.Regossip(ctx, refAtx.SmesherID))
	})

	t.Run("checkpointed", func(t *testing.T) {
		tab := newTestBuilder(t, 5)
		for _, sig := range tab.signers {
			atx := atxs.CheckpointAtx{
				ID:        types.RandomATXID(),
				Epoch:     layer.GetEpoch(),
				SmesherID: sig.NodeID(),
			}
			require.NoError(t, atxs.AddCheckpointed(tab.db, &atx))
			tab.mclock.EXPECT().CurrentLayer().Return(layer)
			require.NoError(t, tab.Regossip(context.Background(), sig.NodeID()))
		}
	})
}

func Test_Builder_Multi_InitialPost(t *testing.T) {
	tab := newTestBuilder(t, 5, WithPoetConfig(
		PoetConfig{
			PhaseShift: layerDuration * 4,
		}))

	var eg errgroup.Group
	for _, sig := range tab.signers {
		eg.Go(func() error {
			numUnits := uint32(12)

			post := &types.Post{
				Indices: types.RandomBytes(10),
				Nonce:   rand.Uint32(),
				Pow:     rand.Uint64(),
			}
			commitmentATX := types.RandomATXID()
			tab.mValidator.EXPECT().
				PostV2(gomock.Any(), sig.NodeID(), commitmentATX, post, shared.ZeroChallenge, numUnits)
			tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge, nil).Return(
				post,
				&types.PostInfo{
					CommitmentATX: commitmentATX,
					Nonce:         new(types.VRFPostIndex),
					NumUnits:      numUnits,
					NodeID:        sig.NodeID(),
					LabelsPerUnit: DefaultPostConfig().LabelsPerUnit,
				},
				nil,
			)
			require.NoError(t, tab.BuildInitialPost(context.Background(), sig.NodeID()))

			// postClient.Proof() should not be called again
			require.NoError(t, tab.BuildInitialPost(context.Background(), sig.NodeID()))
			return nil
		})
	}

	eg.Wait()
}

func Test_Builder_Multi_HappyPath(t *testing.T) {
	layerDuration := 2 * time.Second
	tab := newTestBuilder(t, 3, WithPoetConfig(
		PoetConfig{
			PhaseShift: layerDuration * 4,
			CycleGap:   layerDuration,
		}))

	// step 1: build initial posts
	initialPostChan := make(chan struct{})
	initialPostStep := make(map[types.NodeID]chan struct{})
	initialPost := make(map[types.NodeID]*nipost.Post)
	for _, sig := range tab.signers {
		ch := make(chan struct{})
		initialPostStep[sig.NodeID()] = ch

		dbPost := nipost.Post{
			Indices: types.RandomBytes(10),
			Nonce:   rand.Uint32(),
			Pow:     rand.Uint64(),

			NumUnits:      uint32(12),
			CommitmentATX: types.RandomATXID(),
			VRFNonce:      types.VRFPostIndex(rand.Uint64()),
		}
		initialPost[sig.NodeID()] = &dbPost

		post := &types.Post{
			Indices: dbPost.Indices,
			Nonce:   dbPost.Nonce,
			Pow:     dbPost.Pow,
		}
		tab.mValidator.EXPECT().
			PostV2(gomock.Any(), sig.NodeID(), dbPost.CommitmentATX, post, shared.ZeroChallenge, dbPost.NumUnits)
		tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge, nil).DoAndReturn(
			func(ctx context.Context, _ types.NodeID, _ []byte, _ *types.NIPostChallenge,
			) (*types.Post, *types.PostInfo, error) {
				<-initialPostChan
				close(ch)
				post := &types.Post{
					Indices: dbPost.Indices,
					Nonce:   dbPost.Nonce,
					Pow:     dbPost.Pow,
				}
				postInfo := &types.PostInfo{
					NumUnits:      dbPost.NumUnits,
					CommitmentATX: dbPost.CommitmentATX,
					Nonce:         &dbPost.VRFNonce,
					LabelsPerUnit: DefaultPostConfig().LabelsPerUnit,
				}

				return post, postInfo, nil
			},
		)
	}

	// step 2: build nipost challenge
	nipostChallengeChan := make(chan struct{})
	nipostChallengeStep := make(map[types.NodeID]chan struct{})
	poetRoundEnd := time.Now().Add(1 * time.Second).Add(-tab.poetCfg.PhaseShift) // poetRoundEnd is in 100ms
	for _, sig := range tab.signers {
		ch := make(chan struct{})
		nipostChallengeStep[sig.NodeID()] = ch

		tab.mclock.EXPECT().CurrentLayer().DoAndReturn(
			func() types.LayerID {
				<-nipostChallengeChan
				return postGenesisEpoch.FirstLayer() + 1
			},
		)

		// called twice per id
		tab.mclock.EXPECT().LayerToTime(postGenesisEpoch.FirstLayer()).Return(poetRoundEnd).Times(2)

		// logged once per id
		tab.mclock.EXPECT().CurrentLayer().DoAndReturn(
			func() types.LayerID {
				close(ch)
				return postGenesisEpoch.FirstLayer() + 1
			},
		)

		nipost := initialPost[sig.NodeID()]
		post := &types.Post{
			Indices: nipost.Indices,
			Nonce:   nipost.Nonce,
			Pow:     nipost.Pow,
		}
		tab.mValidator.EXPECT().
			PostV2(gomock.Any(), sig.NodeID(), nipost.CommitmentATX, post, shared.ZeroChallenge, nipost.NumUnits)
	}

	// step 3: create ATX
	nipostChan := make(chan struct{})
	nipostStep := make(map[types.NodeID]chan struct{})
	nipostState := make(map[types.NodeID]*nipost.NIPostState)
	for _, sig := range tab.signers {
		ch := make(chan struct{})
		nipostStep[sig.NodeID()] = ch

		// deadline for create ATX
		tab.mclock.EXPECT().LayerToTime(postGenesisEpoch.Add(2).FirstLayer()).DoAndReturn(
			func(_ types.LayerID) time.Time {
				<-nipostChan
				return time.Now().Add(5 * time.Second)
			},
		)

		post := &wire.PostV1{
			Indices: initialPost[sig.NodeID()].Indices,
			Nonce:   initialPost[sig.NodeID()].Nonce,
			Pow:     initialPost[sig.NodeID()].Pow,
		}
		ref := &wire.NIPostChallengeV1{
			PublishEpoch:     postGenesisEpoch + 1,
			CommitmentATXID:  &initialPost[sig.NodeID()].CommitmentATX,
			Sequence:         0,
			PrevATXID:        types.EmptyATXID,
			PositioningATXID: tab.goldenATXID,
			InitialPost:      post,
		}

		state := &nipost.NIPostState{
			NIPost: &types.NIPost{
				Membership: types.MerkleProof{},
				Post: &types.Post{
					Indices: types.RandomBytes(10),
					Nonce:   rand.Uint32(),
					Pow:     rand.Uint64(),
				},
				PostMetadata: &types.PostMetadata{
					LabelsPerUnit: 128,
					Challenge:     shared.ZeroChallenge,
				},
			},
			NumUnits: 4,
			VRFNonce: types.VRFPostIndex(rand.Uint64()),
		}
		nipostState[sig.NodeID()] = state
		tab.mnipost.EXPECT().
			BuildNIPost(gomock.Any(), sig, ref.Hash(), gomock.Any()).
			DoAndReturn(func(_ context.Context, _ *signing.EdSigner, _ types.Hash32,
				postChallenge *types.NIPostChallenge,
			) (*nipost.NIPostState, error) {
				require.Equal(t, postChallenge.PublishEpoch, ref.PublishEpoch, "publish epoch mismatch")
				return state, nil
			})

		// awaiting atx publication epoch log
		tab.mclock.EXPECT().CurrentLayer().DoAndReturn(
			func() types.LayerID {
				close(ch)
				return postGenesisEpoch.Add(1).FirstLayer()
			},
		)
	}

	// step 4: build and broadcast atx
	atxChan := make(chan struct{})
	atxStep := make(map[types.NodeID]chan struct{})
	var atxMtx sync.Mutex
	atxs := make(map[types.NodeID]wire.ActivationTxV1)
	endChan := make(chan struct{})
	for _, sig := range tab.signers {
		ch := make(chan struct{})
		atxStep[sig.NodeID()] = ch

		tab.mclock.EXPECT().AwaitLayer(postGenesisEpoch.Add(1).FirstLayer()).DoAndReturn(
			func(_ types.LayerID) <-chan struct{} {
				<-atxChan
				ch := make(chan struct{})
				close(ch)
				return ch
			},
		)
		tab.mclock.EXPECT().CurrentLayer().Return(postGenesisEpoch.Add(1).FirstLayer())

		tab.mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
			func(ctx context.Context, _ string, got []byte) error {
				atxMtx.Lock()
				defer atxMtx.Unlock()
				var atx wire.ActivationTxV1
				codec.MustDecode(got, &atx)
				atxs[atx.SmesherID] = atx
				return nil
			},
		)

		// shutdown builder
		tab.mnipost.EXPECT().ResetState(sig.NodeID()).DoAndReturn(
			func(_ types.NodeID) error {
				close(ch)
				<-endChan
				return context.Canceled
			},
		)
	}

	// start smeshing
	require.NoError(t, tab.StartSmeshing(types.Address{}))

	close(initialPostChan) // signal initial post to complete
	for id, ch := range initialPostStep {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			require.FailNowf(t, "timed out waiting for initial post", "node %s", id)
		}
	}

	close(nipostChallengeChan)
	for id, ch := range nipostChallengeStep {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			require.FailNowf(t, "timed out waiting for nipost challenge", "node %s", id)
		}
	}

	close(nipostChan)
	for id, ch := range nipostStep {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			require.FailNowf(t, "timed out waiting for nipost", "node %s", id)
		}
	}

	close(atxChan)
	for id, ch := range atxStep {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			require.FailNowf(t, "timed out waiting for atx publication", "node %s", id)
		}
	}
	close(endChan)

	for _, sig := range tab.signers {
		atx := atxs[sig.NodeID()]
		require.Equal(t, initialPost[sig.NodeID()].Nonce, atx.InitialPost.Nonce)
		require.Equal(t, initialPost[sig.NodeID()].Pow, atx.InitialPost.Pow)
		require.Equal(t, initialPost[sig.NodeID()].Indices, atx.InitialPost.Indices)

		require.Equal(t, initialPost[sig.NodeID()].CommitmentATX, *atx.CommitmentATXID)
		require.Equal(t, postGenesisEpoch+1, atx.PublishEpoch)
		require.Equal(t, types.EmptyATXID, atx.PrevATXID)
		require.Equal(t, tab.goldenATXID, atx.PositioningATXID)
		require.Equal(t, uint64(0), atx.Sequence)

		require.Equal(t, types.Address{}, atx.Coinbase)
		require.Equal(t, nipostState[sig.NodeID()].NumUnits, atx.NumUnits)
		require.Equal(t, nipostState[sig.NodeID()].NIPost, wire.NiPostFromWireV1(atx.NIPost))
		require.Equal(t, sig.NodeID(), *atx.NodeID)
		require.Equal(t, uint64(nipostState[sig.NodeID()].VRFNonce), *atx.VRFNonce)
	}

	// stop smeshing
	require.NoError(t, tab.StopSmeshing(false))
}
