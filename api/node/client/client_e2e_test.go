package client_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/node/client"
	"github.com/spacemeshos/go-spacemesh/api/node/server"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

const retries = 3

func setupE2E(t *testing.T) (activation.AtxService, *activation.MockAtxService) {
	log := zaptest.NewLogger(t)
	actServiceMock := activation.NewMockAtxService(gomock.NewController(t))
	activationServiceServer := server.NewServer(actServiceMock, nil, log.Named("server"))

	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	server := &http.Server{
		Handler: activationServiceServer.IntoHandler(http.NewServeMux()),
	}

	go server.Serve(listener)
	t.Cleanup(func() {
		server.Close()
	})

	cfg := &client.Config{
		RetryMax: retries,
	}
	svc, err := client.NewNodeServiceClient("http://"+listener.Addr().String(), log.Named("server"), cfg)
	require.NoError(t, err)
	return svc, actServiceMock
}

func Test_ActivationService_Atx(t *testing.T) {
	svc, mock := setupE2E(t)

	atxid := types.ATXID{1, 2, 3, 4}

	t.Run("not found", func(t *testing.T) {
		mock.EXPECT().Atx(gomock.Any(), atxid).Return(nil, common.ErrNotFound)
		_, err := svc.Atx(context.Background(), atxid)
		require.ErrorIs(t, err, common.ErrNotFound)
	})

	t.Run("found", func(t *testing.T) {
		atx := &types.ActivationTx{}
		atx.SetID(atxid)
		mock.EXPECT().Atx(gomock.Any(), atxid).Return(atx, nil)
		gotAtx, err := svc.Atx(context.Background(), atxid)
		require.NoError(t, err)
		require.Equal(t, atx, gotAtx)
	})

	t.Run("backend errors", func(t *testing.T) {
		mock.EXPECT().
			Atx(gomock.Any(), atxid).
			Times(retries+1).
			Return(nil, errors.New("ops"))
		_, err := svc.Atx(context.Background(), atxid)
		require.Error(t, err)
	})
}

func Test_ActivationService_PositioningATX(t *testing.T) {
	svc, mock := setupE2E(t)

	t.Run("found", func(t *testing.T) {
		posAtx := types.RandomATXID()
		mock.EXPECT().PositioningATX(gomock.Any(), types.EpochID(77)).Return(posAtx, nil)
		gotAtx, err := svc.PositioningATX(context.Background(), 77)
		require.NoError(t, err)
		require.Equal(t, posAtx, gotAtx)
	})

	t.Run("backend errors", func(t *testing.T) {
		mock.EXPECT().
			PositioningATX(gomock.Any(), types.EpochID(77)).
			Times(retries+1).
			Return(types.EmptyATXID, errors.New("ops"))
		_, err := svc.PositioningATX(context.Background(), 77)
		require.Error(t, err)
	})
}

func Test_ActivationService_LastATX(t *testing.T) {
	svc, mock := setupE2E(t)

	atxid := types.ATXID{1, 2, 3, 4}
	nodeid := types.NodeID{5, 6, 7, 8}

	t.Run("not found", func(t *testing.T) {
		mock.EXPECT().LastATX(gomock.Any(), nodeid).Return(nil, common.ErrNotFound)
		_, err := svc.LastATX(context.Background(), nodeid)
		require.ErrorIs(t, err, common.ErrNotFound)
	})

	t.Run("found", func(t *testing.T) {
		atx := &types.ActivationTx{}
		atx.SetID(atxid)
		mock.EXPECT().LastATX(gomock.Any(), nodeid).Return(atx, nil)
		gotAtx, err := svc.LastATX(context.Background(), nodeid)
		require.NoError(t, err)
		require.Equal(t, atx, gotAtx)
	})

	t.Run("backend errors", func(t *testing.T) {
		mock.EXPECT().
			LastATX(gomock.Any(), nodeid).
			Times(retries+1).
			Return(nil, errors.New("ops"))
		_, err := svc.LastATX(context.Background(), nodeid)
		require.Error(t, err)
	})
}
