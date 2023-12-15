package activation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
)

func TestOffloadingPostVerifier(t *testing.T) {
	proof := shared.Proof{}
	metadata := shared.ProofMetadata{}

	verifier := activation.NewMockPostVerifier(gomock.NewController(t))
	offloadingVerifier := activation.NewOffloadingPostVerifier(verifier, 1, zaptest.NewLogger(t))
	defer offloadingVerifier.Close()
	verifier.EXPECT().Close().Return(nil)

	verifier.EXPECT().Verify(gomock.Any(), &proof, &metadata, gomock.Any()).Return(nil)
	err := offloadingVerifier.Verify(context.Background(), &proof, &metadata)
	require.NoError(t, err)

	verifier.EXPECT().Verify(gomock.Any(), &proof, &metadata, gomock.Any()).Return(errors.New("invalid proof!"))
	err = offloadingVerifier.Verify(context.Background(), &proof, &metadata)
	require.ErrorContains(t, err, "invalid proof!")
}

func TestPostVerifierDetectsInvalidProof(t *testing.T) {
	verifier, err := activation.NewPostVerifier(activation.PostConfig{}, zaptest.NewLogger(t))
	require.NoError(t, err)
	defer verifier.Close()
	require.Error(t, verifier.Verify(context.Background(), &shared.Proof{}, &shared.ProofMetadata{}))
}

func TestPostVerifierVerifyAfterStop(t *testing.T) {
	proof := shared.Proof{}
	metadata := shared.ProofMetadata{}

	verifier := activation.NewMockPostVerifier(gomock.NewController(t))
	offloadingVerifier := activation.NewOffloadingPostVerifier(verifier, 1, zaptest.NewLogger(t))
	defer offloadingVerifier.Close()

	verifier.EXPECT().Verify(gomock.Any(), &proof, &metadata, gomock.Any()).Return(nil)
	err := offloadingVerifier.Verify(context.Background(), &proof, &metadata)
	require.NoError(t, err)

	// Stop the verifier
	verifier.EXPECT().Close().Return(nil)
	offloadingVerifier.Close()

	err = offloadingVerifier.Verify(context.Background(), &proof, &metadata)
	require.EqualError(t, err, "verifier is closed")
}

func TestPostVerifierNoRaceOnClose(t *testing.T) {
	var proof shared.Proof
	var metadata shared.ProofMetadata

	verifier := activation.NewMockPostVerifier(gomock.NewController(t))
	offloadingVerifier := activation.NewOffloadingPostVerifier(verifier, 1, zaptest.NewLogger(t))
	defer offloadingVerifier.Close()
	verifier.EXPECT().Close().AnyTimes().Return(nil)
	verifier.EXPECT().Verify(gomock.Any(), &proof, &metadata, gomock.Any()).AnyTimes().Return(nil)

	// Stop the verifier
	var eg errgroup.Group
	eg.Go(func() error {
		time.Sleep(50 * time.Millisecond)
		return offloadingVerifier.Close()
	})

	for i := 0; i < 50; i++ {
		ms := 10 * i
		eg.Go(func() error {
			time.Sleep(time.Duration(ms) * time.Millisecond)
			return offloadingVerifier.Verify(context.Background(), &proof, &metadata)
		})
	}

	require.EqualError(t, eg.Wait(), "verifier is closed")
}

func TestPostVerifierClose(t *testing.T) {
	verifier := activation.NewMockPostVerifier(gomock.NewController(t))
	// 0 workers - no one will verify the proof
	v := activation.NewOffloadingPostVerifier(verifier, 0, zaptest.NewLogger(t))

	verifier.EXPECT().Close().Return(nil)
	require.NoError(t, v.Close())

	err := v.Verify(context.Background(), &shared.Proof{}, &shared.ProofMetadata{})
	require.EqualError(t, err, "verifier is closed")
}
