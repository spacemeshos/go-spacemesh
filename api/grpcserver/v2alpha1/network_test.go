package v2alpha1

import (
	"context"
	"testing"
	"time"

	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestNetworkService_Info(t *testing.T) {
	ctx := context.Background()
	genesis := time.Unix(genTimeUnix, 0)

	svc := NewNetworkService(genesis, genesisID, layerDuration)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(ctx, t, cfg)
	client := spacemeshv2alpha1.NewNetworkServiceClient(conn)

	t.Run("network info", func(t *testing.T) {
		info, err := client.Info(ctx, &spacemeshv2alpha1.NetworkInfoRequest{})
		require.NoError(t, err)

		require.Equal(t, info.GenesisTime.AsTime().UTC(), genesis.UTC())
		require.Equal(t, info.LayerDuration.AsDuration(), layerDuration)
		require.Equal(t, info.GenesisId, genesisID.Bytes())
		require.Equal(t, info.Hrp, types.NetworkHRP())
		require.Equal(t, info.EffectiveGenesisLayer, types.GetEffectiveGenesis().Uint32())
		require.Equal(t, info.LayersPerEpoch, types.GetLayersPerEpoch())
	})
}
