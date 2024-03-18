package v2alpha1

import (
	"context"
	"fmt"
	"testing"
	"time"

	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestNetworkService_Info(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	genTime := NewMockgenesisTimeAPI(ctrl)
	genesis := time.Unix(genTimeUnix, 0)
	genTime.EXPECT().GenesisTime().Return(genesis)

	svc := NewNetworkService(genTime, genesisID, layerDuration)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(ctx, t, cfg)
	client := spacemeshv2alpha1.NewNetworkServiceClient(conn)

	t.Run("network info", func(t *testing.T) {
		info, err := client.Info(ctx, &emptypb.Empty{})
		require.NoError(t, err)

		fmt.Println(info)
		require.Equal(t, info.GenesisTime, uint64(genesis.Unix()))
		require.Equal(t, info.LayerDuration, uint32(layerDuration.Seconds()))
		require.Equal(t, info.GenesisId, genesisID.Bytes())
		require.Equal(t, info.Hrp, types.NetworkHRP())
		require.Equal(t, info.EffectiveGenesisLayer, types.GetEffectiveGenesis().Uint32())
		require.Equal(t, info.LayersPerEpoch, types.GetLayersPerEpoch())
	})
}
