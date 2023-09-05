package activation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

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
	defer offloadingVerifier.Close()
	verifier.EXPECT().Close().Return(nil)

	verifier.EXPECT().Verify(gomock.Any(), &proof, &metadata, gomock.Any()).Return(nil)
	err := offloadingVerifier.Verify(ctx, &proof, &metadata)
	require.NoError(t, err)

	verifier.EXPECT().Verify(gomock.Any(), &proof, &metadata, gomock.Any()).Return(errors.New("invalid proof!"))
	err = offloadingVerifier.Verify(ctx, &proof, &metadata)
	require.ErrorContains(t, err, "invalid proof!")
}

func TestPostVerifierDetectsInvalidProof(t *testing.T) {
	verifier, err := activation.NewPostVerifier(activation.PostConfig{}, log.NewDefault(t.Name()))
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
	defer offloadingVerifier.Close()
	verifier.EXPECT().Close().Return(nil)

	verifier.EXPECT().Verify(gomock.Any(), &proof, &metadata, gomock.Any()).Return(nil)
	err := offloadingVerifier.Verify(ctx, &proof, &metadata)
	require.NoError(t, err)

	// Stop the verifier
	verifier.EXPECT().Close().Return(nil)
	offloadingVerifier.Close()

	err = offloadingVerifier.Verify(ctx, &proof, &metadata)
	require.EqualError(t, err, "verifier is closed")
}

func TestPostVerifierReturnsOnCtxCanceledWhenBlockedVerifying(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	v := activation.NewOffloadingPostVerifier(
		[]activation.PostVerifier{
			// empty list of verifiers - no one will verify the proof
		}, log.NewDefault(t.Name()))

	require.NoError(t, v.Close())

	err := v.Verify(ctx, &shared.Proof{}, &shared.ProofMetadata{})
	require.EqualError(t, err, "verifier is closed")
}
