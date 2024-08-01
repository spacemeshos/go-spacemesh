package v2alpha1

import (
	"context"
	"testing"
	"time"

	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

func TestNodeService_Status(t *testing.T) {
	ctrl, ctx := gomock.WithContext(context.Background(), t)
	peerCounter := NewMocknodePeerCounter(ctrl)
	meshAPI := NewMocknodeMeshAPI(ctrl)
	syncer := NewMocknodeSyncer(ctrl)
	clock, err := timesync.NewClock(
		timesync.WithLayerDuration(layerDuration),
		timesync.WithTickInterval(1*time.Second),
		timesync.WithGenesisTime(time.Now()),
		timesync.WithLogger(zaptest.NewLogger(t)))
	require.NoError(t, err)
	defer clock.Close()

	svc := NewNodeService(peerCounter, meshAPI, clock, syncer)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(t, cfg)
	client := spacemeshv2alpha1.NewNodeServiceClient(conn)

	t.Run("node status", func(t *testing.T) {
		peerCounter.EXPECT().PeerCount().Return(1)
		meshAPI.EXPECT().LatestLayer().Return(types.LayerID(10))
		meshAPI.EXPECT().LatestLayerInState().Return(types.LayerID(11))
		meshAPI.EXPECT().ProcessedLayer().Return(types.LayerID(12))
		syncer.EXPECT().IsSynced(gomock.Any()).Return(true)

		status, err := client.Status(ctx, &spacemeshv2alpha1.NodeStatusRequest{})
		require.NoError(t, err)

		require.Equal(t, uint64(1), status.ConnectedPeers)
		require.Equal(t, spacemeshv2alpha1.NodeStatusResponse_SYNC_STATUS_SYNCED.String(), status.GetStatus().String())
		require.Equal(t, uint32(10), status.LatestLayer)
		require.Equal(t, uint32(11), status.AppliedLayer)
		require.Equal(t, uint32(12), status.ProcessedLayer)
		require.Equal(t, uint32(0), status.CurrentLayer)
	})
}
