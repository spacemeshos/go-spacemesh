package activation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/log"
)

func TestOffloadingPostVerifier(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	proof := shared.Proof{}
	metadata := shared.ProofMetadata{}

	verifier := activation.NewMockPostVerifier(gomock.NewController(t))
	offloadingVerifier := activation.NewOffloadingPostVerifier(
		[]activation.PostVerifier{verifier},
		log.NewDefault(t.Name()),
	)

	var eg errgroup.Group
	eg.Go(func() error {
		offloadingVerifier.Start(ctx)
		return nil
	})

	{
		verifier.EXPECT().Verify(ctx, &proof, &metadata, gomock.Any()).Return(nil)
		err := offloadingVerifier.Verify(ctx, &proof, &metadata)
		require.NoError(t, err)
	}
	{
		verifier.EXPECT().Verify(ctx, &proof, &metadata, gomock.Any()).Return(errors.New("invalid proof!"))
		err := offloadingVerifier.Verify(ctx, &proof, &metadata)
		require.ErrorContains(t, err, "invalid proof!")
	}

	cancel()
	require.NoError(t, eg.Wait())
}

func TestPostVerfierDetectsInvalidProof(t *testing.T) {
	verifier, err := activation.NewPostVerifier(activation.PostConfig{}, log.NewDefault(t.Name()), nil)
	require.NoError(t, err)
	defer verifier.Close()
	require.Error(t, verifier.Verify(context.Background(), &shared.Proof{}, &shared.ProofMetadata{}))
}

func TestPostVerifierVerifyAfterStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	proof := shared.Proof{}
	metadata := shared.ProofMetadata{}

	verifier := activation.NewMockPostVerifier(gomock.NewController(t))
	offloadingVerifier := activation.NewOffloadingPostVerifier(
		[]activation.PostVerifier{verifier},
		log.NewDefault(t.Name()),
	)

	var eg errgroup.Group
	eg.Go(func() error {
		offloadingVerifier.Start(ctx)
		return nil
	})

	{
		verifier.EXPECT().Verify(ctx, &proof, &metadata, gomock.Any()).Return(nil)
		err := offloadingVerifier.Verify(ctx, &proof, &metadata)
		require.NoError(t, err)
	}
	// Stop the verifier
	cancel()
	require.NoError(t, eg.Wait())

	{
		err := offloadingVerifier.Verify(ctx, &proof, &metadata)
		require.ErrorIs(t, err, context.Canceled)
	}
}

func TestPostVerifierReturnsOnCtxCanceledWhenBlockedVerifying(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	v := activation.NewOffloadingPostVerifier(
		[]activation.PostVerifier{
			// empty list of verifiers - no one will verify the proof
		}, log.NewDefault(t.Name()))

	var eg errgroup.Group
	eg.Go(func() error {
		v.Start(ctx)
		return nil
	})

	cancel()
	err := v.Verify(ctx, &shared.Proof{}, &shared.ProofMetadata{})
	require.ErrorIs(t, err, context.Canceled)

	require.NoError(t, eg.Wait())
}
