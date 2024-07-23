package grpcserver

import (
	"context"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type nodeServiceConn struct {
	pb.NodeServiceClient

	meshAPI     *MockmeshAPI
	genTime     *MockgenesisTimeAPI
	peerCounter *MockpeerCounter
	syncer      *Mocksyncer
}

func setupNodeService(t *testing.T) (*nodeServiceConn, context.Context) {
	ctrl, mockCtx := gomock.WithContext(context.Background(), t)
	peerCounter := NewMockpeerCounter(ctrl)
	meshAPI := NewMockmeshAPI(ctrl)
	genTime := NewMockgenesisTimeAPI(ctrl)
	syncer := NewMocksyncer(ctrl)

	version := "v0.0.0"
	build := "cafebabe"
	grpcService := NewNodeService(peerCounter, meshAPI, genTime, syncer, version, build)
	cfg, cleanup := launchServer(t, grpcService)
	t.Cleanup(cleanup)

	conn := dialGrpc(t, cfg)
	client := pb.NewNodeServiceClient(conn)

	return &nodeServiceConn{
		NodeServiceClient: client,

		meshAPI:     meshAPI,
		genTime:     genTime,
		peerCounter: peerCounter,
		syncer:      syncer,
	}, mockCtx
}

func TestNodeService(t *testing.T) {
	t.Run("Echo", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupNodeService(t)

		const message = "Hello World"
		res, err := c.Echo(ctx, &pb.EchoRequest{
			Msg: &pb.SimpleString{Value: message},
		})
		require.NoError(t, err)
		require.Equal(t, message, res.Msg.Value)

		// now try sending bad payloads
		_, err = c.Echo(ctx, &pb.EchoRequest{Msg: nil})
		require.Error(t, err)
		grpcStatus, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, grpcStatus.Code())
		require.Equal(t, "Must include `Msg`", grpcStatus.Message())

		_, err = c.Echo(ctx, &pb.EchoRequest{})
		require.Error(t, err)
		grpcStatus, ok = status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, grpcStatus.Code())
		require.Equal(t, "Must include `Msg`", grpcStatus.Message())
	})
	t.Run("Version", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupNodeService(t)

		res, err := c.Version(ctx, &emptypb.Empty{})
		require.NoError(t, err)
		require.Equal(t, "v0.0.0", res.VersionString.Value)
	})
	t.Run("Build", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupNodeService(t)

		res, err := c.Build(ctx, &emptypb.Empty{})
		require.NoError(t, err)
		require.Equal(t, "cafebabe", res.BuildString.Value)
	})
	t.Run("Status genesis", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupNodeService(t)

		// First do a mock checking during a genesis layer
		// During genesis all layers should be set to current layer
		layerLatest := types.LayerID(10)
		c.meshAPI.EXPECT().LatestLayer().Return(layerLatest)
		layerCurrent := types.LayerID(layersPerEpoch) // end of first epoch
		c.genTime.EXPECT().CurrentLayer().Return(layerCurrent)
		c.peerCounter.EXPECT().PeerCount().Return(0)
		c.syncer.EXPECT().IsSynced(gomock.Any()).Return(false)

		res, err := c.Status(ctx, &pb.StatusRequest{})
		require.NoError(t, err)
		require.Equal(t, uint64(0), res.Status.ConnectedPeers)
		require.False(t, res.Status.IsSynced)
		require.Equal(t, layerLatest.Uint32(), res.Status.SyncedLayer.Number)
		require.Equal(t, layerCurrent.Uint32(), res.Status.TopLayer.Number)
		require.Equal(t, layerLatest.Uint32(), res.Status.VerifiedLayer.Number)
	})
	t.Run("Status post-genesis", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupNodeService(t)

		// Now do a mock check post-genesis
		layerLatest := types.LayerID(10)
		c.meshAPI.EXPECT().LatestLayer().Return(layerLatest)
		layerCurrent = types.LayerID(12)
		c.genTime.EXPECT().CurrentLayer().Return(layerCurrent)
		layerVerified := types.LayerID(8)
		c.meshAPI.EXPECT().LatestLayerInState().Return(layerVerified)
		c.peerCounter.EXPECT().PeerCount().Return(100)
		c.syncer.EXPECT().IsSynced(gomock.Any()).Return(false)

		res, err := c.Status(ctx, &pb.StatusRequest{})
		require.NoError(t, err)
		require.Equal(t, uint64(100), res.Status.ConnectedPeers)
		require.False(t, res.Status.IsSynced)
		require.Equal(t, layerLatest.Uint32(), res.Status.SyncedLayer.Number)
		require.Equal(t, layerCurrent.Uint32(), res.Status.TopLayer.Number)
		require.Equal(t, layerVerified.Uint32(), res.Status.VerifiedLayer.Number)
	})
	t.Run("NodeInfo", func(t *testing.T) {
		t.Parallel()
		c, ctx := setupNodeService(t)

		resp, err := c.NodeInfo(ctx, &emptypb.Empty{})
		require.NoError(t, err)
		require.Equal(t, resp.Hrp, types.NetworkHRP())
		require.Equal(t, resp.FirstGenesis, types.FirstEffectiveGenesis().Uint32())
		require.Equal(t, resp.EffectiveGenesis, types.GetEffectiveGenesis().Uint32())
		require.Equal(t, resp.EpochSize, types.GetLayersPerEpoch())
	})
	// NOTE: ErrorStream and StatusStream have comprehensive, E2E tests in cmd/node/node_test.go.
}
