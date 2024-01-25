package activation

import (
	"context"
	"testing"

	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
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
