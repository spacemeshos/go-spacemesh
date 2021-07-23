package peersync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/timesync/peersync/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func defaultPeersProvider(tb testing.TB, added, expired chan p2pcrypto.PublicKey) PeersProvider {
	tb.Helper()
	ctrl := gomock.NewController(tb)
	pp := mocks.NewMockPeersProvider(ctrl)
	pp.EXPECT().SubscribePeerEvents().AnyTimes().Return(added, expired)
	return pp
}

func TestSyncGetOffset(t *testing.T) {
	var (
		start           = time.Time{}
		roundStartTime  = start.Add(10 * time.Second)
		peerResponse    = start.Add(30 * time.Second)
		responseReceive = start.Add(40 * time.Second)
	)

	peers := []p2pcrypto.PublicKey{p2pcrypto.NewRandomPubkey(), p2pcrypto.NewRandomPubkey(), p2pcrypto.NewRandomPubkey()}
	resp := Response{
		Timestamp: peerResponse.UnixNano(),
	}
	respBuf, err := types.InterfaceToBytes(resp)
	require.NoError(t, err)

	t.Run("Success", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		srv := mocks.NewMockMessageServer(ctrl)
		tm := mocks.NewMockTime(ctrl)
		srv.EXPECT().RegisterBytesMsgHandler(gomock.Any(), gomock.Any())

		tm.EXPECT().Now().Return(roundStartTime)
		for _, peer := range peers {
			srv.EXPECT().SendRequest(gomock.Any(), server.RequestTimeSync, gomock.Any(), peer, gomock.Any(), gomock.Any()).Do(
				func(_ context.Context, _ server.MessageType, _ []byte, _ p2pcrypto.PublicKey, handler func([]byte), _ func(error)) {
					handler(respBuf)
				},
			).Return(nil)
			tm.EXPECT().Now().Return(responseReceive)
		}
		sync := New(srv, defaultPeersProvider(t, nil, nil),
			WithTime(tm),
		)
		offset, err := sync.GetOffset(context.TODO(), 0, peers)
		require.NoError(t, err)
		require.Equal(t, 5*time.Second, offset)
	})

	t.Run("Failure", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		srv := mocks.NewMockMessageServer(ctrl)
		tm := mocks.NewMockTime(ctrl)
		srv.EXPECT().RegisterBytesMsgHandler(gomock.Any(), gomock.Any())

		tm.EXPECT().Now().Return(roundStartTime)
		for _, peer := range peers {
			srv.EXPECT().SendRequest(gomock.Any(), server.RequestTimeSync, gomock.Any(), peer, gomock.Any(), gomock.Any()).Do(
				func(_ context.Context, _ server.MessageType, _ []byte, _ p2pcrypto.PublicKey, _ func([]byte), errhandler func(error)) {
					errhandler(errors.New("test"))
				},
			).Return(nil)
		}
		sync := New(srv, defaultPeersProvider(t, nil, nil),
			WithTime(tm),
		)
		offset, err := sync.GetOffset(context.TODO(), 0, peers)
		require.ErrorIs(t, err, ErrTimesyncTimeout)
		require.Equal(t, time.Duration(0), offset)
	})
}

func TestSyncTerminateOnError(t *testing.T) {
	// NOTE(dshulyak) -coverprofile doesn't seem to track code that is executed no in the main goroutine

	config := DefaultConfig()
	config.MaxClockOffset = 1 * time.Second
	config.MaxOffsetErrors = 1
	config.RoundInterval = time.Duration(0)

	var (
		start           = time.Time{}
		roundStartTime  = start.Add(10 * time.Second)
		peerResponse    = start.Add(30 * time.Second)
		responseReceive = start.Add(30 * time.Second)
		peersCount      = 3
	)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	srv := mocks.NewMockMessageServer(ctrl)
	tm := mocks.NewMockTime(ctrl)

	added := make(chan p2pcrypto.PublicKey, 3)

	srv.EXPECT().RegisterBytesMsgHandler(gomock.Any(), gomock.Any())
	sync := New(srv, defaultPeersProvider(t, added, nil),
		WithTime(tm),
		WithConfig(config),
	)
	tm.EXPECT().Now().Return(roundStartTime)
	sync.Start()
	t.Cleanup(sync.Stop)
	for i := 0; i < peersCount; i++ {
		peer := p2pcrypto.NewRandomPubkey()
		srv.EXPECT().SendRequest(gomock.Any(), server.RequestTimeSync, gomock.Any(), peer, gomock.Any(), gomock.Any()).Do(
			func(_ context.Context, _ server.MessageType, buf []byte, _ p2pcrypto.PublicKey, handler func([]byte), _ func(error)) {
				var req Request
				assert.NoError(t, types.BytesToInterface(buf, &req))
				resp := Response{
					ID:        req.ID,
					Timestamp: peerResponse.UnixNano(),
				}
				respBuf, err := types.InterfaceToBytes(resp)
				assert.NoError(t, err)
				handler(respBuf)
			},
		).Return(nil)
		tm.EXPECT().Now().Return(responseReceive)
		added <- peer
	}
	errors := make(chan error, 1)
	go func() {
		errors <- sync.Wait()
	}()
	select {
	case err := <-errors:
		require.ErrorIs(t, err, ErrPeersNotSynced)
	case <-time.After(100 * time.Millisecond):
		require.FailNow(t, "timed out waiting for sync to fail")
	}
}
