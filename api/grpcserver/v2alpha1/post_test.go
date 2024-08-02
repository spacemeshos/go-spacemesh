package v2alpha1

import (
	"context"
	"math/rand"
	"testing"

	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/spacemeshos/post/config"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
)

func TestPostService_Config(t *testing.T) {
	ctx := context.Background()

	postConfig := activation.PostConfig{
		MinNumUnits:   rand.Uint32(),
		MaxNumUnits:   rand.Uint32(),
		LabelsPerUnit: rand.Uint64(),
		K1:            uint(rand.Uint32()),
		K2:            uint(rand.Uint32()),
	}

	svc := NewPostService(postConfig)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	conn := dialGrpc(t, cfg)
	client := spacemeshv2alpha1.NewPostServiceClient(conn)

	t.Run("post config", func(t *testing.T) {
		res, err := client.Config(ctx, &spacemeshv2alpha1.PostConfigRequest{})
		require.NoError(t, err)

		require.Equal(t, uint32(config.BitsPerLabel), res.BitsPerLabel)
		require.Equal(t, postConfig.LabelsPerUnit, res.LabelsPerUnit)
		require.Equal(t, postConfig.MinNumUnits, res.MinNumUnits)
		require.Equal(t, postConfig.MaxNumUnits, res.MaxNumUnits)
		require.Equal(t, uint32(postConfig.K1), res.K1)
		require.Equal(t, uint32(postConfig.K2), res.K2)
	})
}
