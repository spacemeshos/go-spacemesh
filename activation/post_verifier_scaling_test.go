package activation

import (
	"context"
	"testing"
	"time"

	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
)

func TestAutoScaling(t *testing.T) {
	events.InitializeReporter()
	t.Cleanup(events.CloseEventReporter)

	mScalerCtrl := gomock.NewController(t)
	mockScaler := NewMockscaler(mScalerCtrl)
	gomock.InOrder(
		mockScaler.EXPECT().scale(1), // on start
		mockScaler.EXPECT().scale(5), // on complete
		mockScaler.EXPECT().scale(5), // on failed
	)

	stop := make(chan struct{})
	var eg errgroup.Group
	autoscaler := newAutoscaler(zaptest.NewLogger(t), NewPostStates(zaptest.NewLogger(t)), 5)
	err := autoscaler.subscribe()
	require.NoError(t, err)
	eg.Go(func() error {
		autoscaler.run(stop, mockScaler, 1, 5)
		return nil
	})

	events.EmitInitComplete(types.EmptyNodeID) // it shouldn't care about this event
	events.EmitPostStart(types.EmptyNodeID, nil)
	events.EmitPostComplete(types.EmptyNodeID, nil)
	events.EmitPostFailure(types.EmptyNodeID)
	require.Eventually(t, mScalerCtrl.Satisfied, time.Second, 10*time.Millisecond)

	close(stop)
	eg.Wait()
}

func TestAutoScaling_ScalesDownWhenAllCompleted(t *testing.T) {
	events.InitializeReporter()
	t.Cleanup(events.CloseEventReporter)

	mScalerCtrl := gomock.NewController(t)
	mockScaler := NewMockscaler(mScalerCtrl)
	gomock.InOrder(
		// on each start
		mockScaler.EXPECT().scale(1).Times(2),
		// when all completed
		mockScaler.EXPECT().scale(5),
	)

	stop := make(chan struct{})
	var eg errgroup.Group
	autoscaler := newAutoscaler(zaptest.NewLogger(t), NewPostStates(zaptest.NewLogger(t)), 5)
	err := autoscaler.subscribe()
	require.NoError(t, err)
	eg.Go(func() error {
		autoscaler.run(stop, mockScaler, 1, 5)
		return nil
	})

	nodeID := types.RandomNodeID()

	events.EmitPostStart(nodeID, nil)
	events.EmitPostStart(types.EmptyNodeID, nil)
	events.EmitPostComplete(types.EmptyNodeID, nil)
	events.EmitPostComplete(nodeID, nil)

	require.Eventually(t, mScalerCtrl.Satisfied, time.Hour, 10*time.Millisecond)

	close(stop)
	eg.Wait()
}

func TestPostVerifierScaling(t *testing.T) {
	// 0 workers - no one will verify the proof
	mockVerifier := NewMockPostVerifier(gomock.NewController(t))
	v := newOffloadingPostVerifier(mockVerifier, 0, zaptest.NewLogger(t))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := v.Verify(ctx, &shared.Proof{}, &shared.ProofMetadata{})
	require.ErrorIs(t, err, context.DeadlineExceeded)

	mockVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	v.scale(1)
	err = v.Verify(context.Background(), &shared.Proof{}, &shared.ProofMetadata{})
	require.NoError(t, err)

	v.scale(0)

	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err = v.Verify(ctx, &shared.Proof{}, &shared.ProofMetadata{})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestAutoScalingOverflow(t *testing.T) {
	events.InitializeReporter()
	t.Cleanup(events.CloseEventReporter)

	t.Run("overflow when some are proving", func(t *testing.T) {
		mScalerCtrl := gomock.NewController(t)
		mockScaler := NewMockscaler(mScalerCtrl)
		// on subscription restart go back to `min`
		mockScaler.EXPECT().scale(1)

		stop := make(chan struct{})
		var eg errgroup.Group

		mPostStates := mocks.NewMockpostStatesGetter(gomock.NewController(t))
		autoscaler := newAutoscaler(zaptest.NewLogger(t), mPostStates, 1)
		err := autoscaler.subscribe()
		require.NoError(t, err)
		mSub := mocks.NewMocksubscription[events.UserEvent](gomock.NewController(t))
		autoscaler.sub = mSub

		full := make(chan struct{})
		close(full)
		mSub.EXPECT().Full().Return(full) // overflow
		mSub.EXPECT().Out().Return(make(<-chan events.UserEvent))
		mPostStates.EXPECT().Get().Return(map[types.NodeID]types.PostState{types.EmptyNodeID: types.PostStateProving})

		eg.Go(func() error {
			autoscaler.run(stop, mockScaler, 1, 5)
			return nil
		})

		require.Eventually(t, mScalerCtrl.Satisfied, time.Second*10, 10*time.Millisecond)

		close(stop)
		eg.Wait()
	})
	t.Run("overflow when all are idle", func(t *testing.T) {
		mScalerCtrl := gomock.NewController(t)
		mockScaler := NewMockscaler(mScalerCtrl)
		// on subscription restart go back to `target`
		mockScaler.EXPECT().scale(5)

		stop := make(chan struct{})
		var eg errgroup.Group
		mPostStates := mocks.NewMockpostStatesGetter(gomock.NewController(t))
		autoscaler := newAutoscaler(zaptest.NewLogger(t), mPostStates, 1)
		mSub := mocks.NewMocksubscription[events.UserEvent](gomock.NewController(t))
		autoscaler.sub = mSub

		full := make(chan struct{})
		close(full)
		mSub.EXPECT().Full().Return(full) // overflow
		mSub.EXPECT().Out().Return(make(<-chan events.UserEvent))
		mPostStates.EXPECT().Get() // no one is proving

		eg.Go(func() error {
			autoscaler.run(stop, mockScaler, 1, 5)
			return nil
		})

		require.Eventually(t, mScalerCtrl.Satisfied, time.Second*10, 10*time.Millisecond)

		close(stop)
		eg.Wait()
	})
}
