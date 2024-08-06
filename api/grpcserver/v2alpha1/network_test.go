package v2alpha1

import (
	"context"
	"testing"
	"time"

	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
)

func TestNetworkService_Info(t *testing.T) {
	ctx := context.Background()
	genesis := time.Unix(genTimeUnix, 0)
	c := config.DefaultTestConfig()

	svc := NewNetworkService(genesis, &c)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(t, cfg)
	client := spacemeshv2alpha1.NewNetworkServiceClient(conn)

	t.Run("network info", func(t *testing.T) {
		info, err := client.Info(ctx, &spacemeshv2alpha1.NetworkInfoRequest{})
		require.NoError(t, err)

		require.Equal(t, genesis.UTC(), info.GenesisTime.AsTime().UTC())
		require.Equal(t, c.LayerDuration, info.LayerDuration.AsDuration())
		require.Equal(t, c.Genesis.GenesisID().Bytes(), info.GenesisId)
		require.Equal(t, types.NetworkHRP(), info.Hrp)
		require.Equal(t, types.GetEffectiveGenesis().Uint32(), info.EffectiveGenesisLayer)
		require.Equal(t, types.GetLayersPerEpoch(), info.LayersPerEpoch)
	})
}
