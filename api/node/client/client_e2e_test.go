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
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare3"
	pubsub "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
)

const retries = 3

type mocks struct {
	atxService *activation.MockAtxService
	poetDb     *server.MockpoetDB
	hare       *server.Mockhare
	publisher  *pubsub.MockPublisher
}

func setupE2E(t *testing.T) (*client.NodeService, *mocks) {
	log := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	m := &mocks{
		atxService: activation.NewMockAtxService(ctrl),
		poetDb:     server.NewMockpoetDB(ctrl),
		hare:       server.NewMockhare(ctrl),
		publisher:  pubsub.NewMockPublisher(ctrl),
	}

	activationServiceServer := server.NewServer(m.atxService, m.publisher, m.poetDb, m.hare, log.Named("server"))

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
	return svc, m
}

func Test_ActivationService_Atx(t *testing.T) {
	svc, mock := setupE2E(t)

	atxid := types.ATXID{1, 2, 3, 4}

	t.Run("not found", func(t *testing.T) {
		mock.atxService.EXPECT().Atx(gomock.Any(), atxid).Return(nil, activation.ErrNotFound)
		_, err := svc.Atx(context.Background(), atxid)
		require.ErrorIs(t, err, activation.ErrNotFound)
	})

	t.Run("found", func(t *testing.T) {
		atx := &types.ActivationTx{}
		atx.SetID(atxid)
		mock.atxService.EXPECT().Atx(gomock.Any(), atxid).Return(atx, nil)
		gotAtx, err := svc.Atx(context.Background(), atxid)
		require.NoError(t, err)
		require.Equal(t, atx, gotAtx)
	})

	t.Run("backend errors", func(t *testing.T) {
		mock.atxService.EXPECT().
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
		mock.atxService.EXPECT().PositioningATX(gomock.Any(), types.EpochID(77)).Return(posAtx, nil)
		gotAtx, err := svc.PositioningATX(context.Background(), 77)
		require.NoError(t, err)
		require.Equal(t, posAtx, gotAtx)
	})

	t.Run("backend errors", func(t *testing.T) {
		mock.atxService.EXPECT().
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
		mock.atxService.EXPECT().LastATX(gomock.Any(), nodeid).Return(nil, activation.ErrNotFound)
		_, err := svc.LastATX(context.Background(), nodeid)
		require.ErrorIs(t, err, activation.ErrNotFound)
	})

	t.Run("found", func(t *testing.T) {
		atx := &types.ActivationTx{}
		atx.SetID(atxid)
		mock.atxService.EXPECT().LastATX(gomock.Any(), nodeid).Return(atx, nil)
		gotAtx, err := svc.LastATX(context.Background(), nodeid)
		require.NoError(t, err)
		require.Equal(t, atx, gotAtx)
	})

	t.Run("backend errors", func(t *testing.T) {
		mock.atxService.EXPECT().
			LastATX(gomock.Any(), nodeid).
			Times(retries+1).
			Return(nil, errors.New("ops"))
		_, err := svc.LastATX(context.Background(), nodeid)
		require.Error(t, err)
	})
}

func Test_StoringPoetProof(t *testing.T) {
	svc, mocks := setupE2E(t)

	proof := types.PoetProofMessage{
		RoundID: "some round",
	}
	mocks.poetDb.EXPECT().ValidateAndStore(gomock.Any(), &proof)
	svc.StorePoetProof(context.Background(), &proof)
}

func Test_Hare(t *testing.T) {
	svc, mock := setupE2E(t)
	t.Run("total weight", func(t *testing.T) {
		val := uint64(11)
		mock.hare.EXPECT().TotalWeight(gomock.Any(), gomock.Any()).Return(val, nil)
		v, err := svc.TotalWeight(context.Background(), 112)
		require.Equal(t, v, val)
		require.NoError(t, err)
	})
	t.Run("miner weight", func(t *testing.T) {
		val := uint64(101)
		mock.hare.EXPECT().MinerWeight(gomock.Any(), gomock.Any(), gomock.Any()).Return(val, nil)
		v, err := svc.MinerWeight(context.Background(), 113, types.NodeID{})
		require.Equal(t, v, val)
		require.NoError(t, err)
	})
	t.Run("beacon", func(t *testing.T) {
		beacon := types.Beacon{12, 12, 12, 12}
		mock.hare.EXPECT().Beacon(gomock.Any(), gomock.Any()).Return(beacon, nil)
		v, err := svc.Beacon(context.Background(), types.EpochID(15))
		require.Equal(t, v, beacon)
		require.NoError(t, err)
	})
	t.Run("hare message", func(t *testing.T) {
		exp := make([]byte, 182)
		mock.hare.EXPECT().RoundMessage(gomock.Any(), gomock.Any()).Return(&hare3.Message{})
		v, err := svc.GetHareMessage(context.Background(), types.LayerID(113), hare3.IterRound{})
		require.Equal(t, exp, v)
		require.NoError(t, err)
	})
}
