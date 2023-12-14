package activation

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/events"
)

func TestAutoScaling(t *testing.T) {
	events.InitializeReporter()
	t.Cleanup(events.CloseEventReporter)

	mockScaler := NewMockscaler(gomock.NewController(t))
	var done atomic.Bool
	gomock.InOrder(
		mockScaler.EXPECT().scale(1),                                    // on start
		mockScaler.EXPECT().scale(5),                                    // on complete
		mockScaler.EXPECT().scale(5).Do(func(int) { done.Store(true) }), // on failed
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var eg errgroup.Group
	autoscaler, err := newAutoscaler()
	require.NoError(t, err)
	eg.Go(func() error {
		return autoscaler.run(ctx, mockScaler, 1, 5)
	})

	events.EmitPostStart(nil)
	events.EmitPostComplete(nil)
	events.EmitPostFailure()
	require.Eventually(t, done.Load, time.Second, 5*time.Millisecond)

	cancel()
	eg.Wait()
}

func TestPostVerifierScaling(t *testing.T) {
	// 0 workers - no one will verify the proof
	mockVerifier := NewMockPostVerifier(gomock.NewController(t))
	v := NewOffloadingPostVerifier(mockVerifier, 0, zaptest.NewLogger(t))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := v.Verify(ctx, &shared.Proof{}, &shared.ProofMetadata{})
	require.Error(t, err, context.Canceled)

	mockVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	v.scale(1)
	err = v.Verify(context.Background(), &shared.Proof{}, &shared.ProofMetadata{})
	require.NoError(t, err)

	v.scale(0)
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err = v.Verify(ctx, &shared.Proof{}, &shared.ProofMetadata{})
	require.Error(t, err, context.Canceled)
}
