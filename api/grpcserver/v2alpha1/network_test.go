package v2alpha1

import (
	"context"
	"testing"
	"time"

	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestNetworkService_Info(t *testing.T) {
	ctx := context.Background()
	genesis := time.Unix(genTimeUnix, 0)
	postConfig := activation.DefaultPostConfig()

	svc := NewNetworkService(genesis, genesisID, layerDuration, postConfig.LabelsPerUnit)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(t, cfg)
	client := spacemeshv2alpha1.NewNetworkServiceClient(conn)

	t.Run("network info", func(t *testing.T) {
		info, err := client.Info(ctx, &spacemeshv2alpha1.NetworkInfoRequest{})
		require.NoError(t, err)

		require.Equal(t, genesis.UTC(), info.GenesisTime.AsTime().UTC())
		require.Equal(t, layerDuration, info.LayerDuration.AsDuration())
		require.Equal(t, genesisID.Bytes(), info.GenesisId)
		require.Equal(t, types.NetworkHRP(), info.Hrp)
		require.Equal(t, types.GetEffectiveGenesis().Uint32(), info.EffectiveGenesisLayer)
		require.Equal(t, types.GetLayersPerEpoch(), info.LayersPerEpoch)
		require.Equal(t, postConfig.LabelsPerUnit, info.LabelsPerUnit)
	})
}
