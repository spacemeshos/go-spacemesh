package v2beta1

import (
	"context"
	"testing"
	"time"

	spacemeshv2beta1 "github.com/spacemeshos/api/release/go/spacemesh/v2beta1"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
)

func TestNetworkService_Info(t *testing.T) {
	ctx := context.Background()
	genesis := time.Unix(genTimeUnix, 0)
	c := config.DefaultTestConfig(t)

	svc := NewNetworkService(genesis, &c)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(t, cfg)
	client := spacemeshv2beta1.NewNetworkServiceClient(conn)

	t.Run("network info", func(t *testing.T) {
		info, err := client.Info(ctx, &spacemeshv2beta1.NetworkInfoRequest{})
		require.NoError(t, err)

		require.Equal(t, genesis.UTC(), info.GenesisTime.AsTime().UTC())
		require.Equal(t, c.LayerDuration, info.LayerDuration.AsDuration())
		require.Equal(t, c.Genesis.GenesisID().Bytes(), info.GenesisId)
		require.Equal(t, types.NetworkHRP(), info.Hrp)
		require.Equal(t, types.GetEffectiveGenesis().Uint32(), info.EffectiveGenesisLayer)
		require.Equal(t, types.GetLayersPerEpoch(), info.LayersPerEpoch)
		require.Equal(t, c.POST.LabelsPerUnit, info.LabelsPerUnit)
	})
}
