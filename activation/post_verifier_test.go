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
		verifier.EXPECT().Verify(&proof, &metadata, gomock.Any()).Return(nil)
		err := offloadingVerifier.Verify(&proof, &metadata)
		require.NoError(t, err)
	}
	{
		verifier.EXPECT().Verify(&proof, &metadata, gomock.Any()).Return(errors.New("invalid proof!"))
		err := offloadingVerifier.Verify(&proof, &metadata)
		require.ErrorContains(t, err, "invalid proof!")
	}

	cancel()
	require.NoError(t, eg.Wait())
}

func TestPostVerfierDetectsInvalidProof(t *testing.T) {
	verifier := activation.NewPostVerifier(activation.PostConfig{}, log.NewDefault(t.Name()))
	require.Error(t, verifier.Verify(&shared.Proof{}, &shared.ProofMetadata{}))
}
