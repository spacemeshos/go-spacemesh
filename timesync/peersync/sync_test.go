package peersync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/timesync/peersync/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ service.DirectMessage = (*directMessage)(nil)

type directMessage service.DataMsgWrapper

func (d *directMessage) Data() service.Data {
	return (*service.DataMsgWrapper)(d)
}

func (d *directMessage) Bytes() []byte {
	return (*service.DataMsgWrapper)(d).Bytes()
}

func (d *directMessage) Sender() p2pcrypto.PublicKey {
	return nil
}

func (d *directMessage) Metadata() service.P2PMetadata {
	return service.P2PMetadata{}
}

func TestSyncGetOffset(t *testing.T) {
	var (
		start           = time.Time{}
		roundStartTime  = start.Add(10 * time.Second)
		peerResponse    = start.Add(30 * time.Second)
		responseReceive = start.Add(40 * time.Second)
	)

	peers := []p2pcrypto.PublicKey{
		p2pcrypto.NewRandomPubkey(),
		p2pcrypto.NewRandomPubkey(),
		p2pcrypto.NewRandomPubkey(),
	}
	resp := Response{
		Timestamp: peerResponse.UnixNano(),
	}
	respBuf, err := types.InterfaceToBytes(resp)
	require.NoError(t, err)
	receive := make(chan service.DirectMessage, len(peers))

	t.Run("Success", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		network := mocks.NewMockNetwork(ctrl)
		tm := mocks.NewMockTime(ctrl)
		network.EXPECT().RegisterDirectProtocolWithChannel(protocolName, gomock.Any()).Return(receive)

		tm.EXPECT().Now().Return(roundStartTime)
		for _, peer := range peers {
			network.EXPECT().SendWrappedMessage(gomock.Any(), peer, protocolName, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ p2pcrypto.PublicKey, _ string, msg *service.DataMsgWrapper) error {
					receive <- (*directMessage)(&service.DataMsgWrapper{
						ReqID:   msg.ReqID,
						Payload: respBuf,
					})
					return nil
				},
			)
			tm.EXPECT().Now().Return(responseReceive)
		}
		sync := New(network,
			WithTime(tm),
		)
		offset, err := sync.GetOffset(context.TODO(), 0, peers)
		require.NoError(t, err)
		require.Equal(t, 5*time.Second, offset)
	})

	t.Run("Failure", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		network := mocks.NewMockNetwork(ctrl)
		tm := mocks.NewMockTime(ctrl)
		network.EXPECT().RegisterDirectProtocolWithChannel(protocolName, gomock.Any()).Return(receive)

		tm.EXPECT().Now().Return(roundStartTime)
		for _, peer := range peers {
			network.EXPECT().SendWrappedMessage(gomock.Any(), peer, protocolName, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ p2pcrypto.PublicKey, _ string, msg *service.DataMsgWrapper) error {
					return errors.New("test")
				},
			)
		}
		conf := DefaultConfig()
		conf.RequiredResponses = 0
		sync := New(network,
			WithTime(tm),
			WithConfig(conf),
		)
		offset, err := sync.GetOffset(context.TODO(), 0, peers)
		require.ErrorIs(t, err, ErrTimesyncFailed)
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
	network := mocks.NewMockNetwork(ctrl)
	tm := mocks.NewMockTime(ctrl)
	receive := make(chan service.DirectMessage, peersCount)
	network.EXPECT().RegisterDirectProtocolWithChannel(protocolName, gomock.Any()).Return(receive)
	added := make(chan p2pcrypto.PublicKey, peersCount)
	network.EXPECT().SubscribePeerEvents().Return(added, nil)

	sync := New(network,
		WithTime(tm),
		WithConfig(config),
	)
	tm.EXPECT().Now().Return(roundStartTime)
	sync.Start()
	t.Cleanup(sync.Stop)
	for i := 0; i < peersCount; i++ {
		peer := p2pcrypto.NewRandomPubkey()
		network.EXPECT().SendWrappedMessage(gomock.Any(), peer, protocolName, gomock.Any()).DoAndReturn(
			func(_ context.Context, _ p2pcrypto.PublicKey, _ string, msg *service.DataMsgWrapper) error {
				var req Request
				assert.NoError(t, types.BytesToInterface(msg.Payload, &req))
				resp := Response{
					ID:        req.ID,
					Timestamp: peerResponse.UnixNano(),
				}
				respBuf, err := types.InterfaceToBytes(resp)
				assert.NoError(t, err)

				receive <- (*directMessage)(&service.DataMsgWrapper{
					ReqID:   msg.ReqID,
					Payload: respBuf,
				})
				return nil
			},
		)
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
