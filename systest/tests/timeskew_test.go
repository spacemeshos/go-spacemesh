package tests

import (
	"context"
	"testing"

	spacemeshv1 "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/systest/chaos"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
	"golang.org/x/sync/errgroup"

	"github.com/stretchr/testify/require"
)

func TestShortTimeskew(t *testing.T) {
	tctx := testcontext.New(t)
	cl, err := cluster.Default(tctx, cluster.WithKeys(10))
	require.NoError(t, err)

	failed := int(0.2 * float64(tctx.ClusterSize))
	eg, ctx := errgroup.WithContext(tctx)
	scheduleChaos(ctx, eg, cl.Client(0), 14, 17, func(ctx context.Context) (chaos.Teardown, error) {
		names := []string{}
		for i := 1; i <= failed; i++ {
			names = append(names, cl.Client(cl.Total()-i).Name)
		}
		return chaos.Timeskew(tctx, "skew5s", "-5s", names...)
	})
	watchLayers(ctx, eg, cl.Client(0), func(layer *spacemeshv1.LayerStreamResponse) (bool, error) {
		if layer.Layer.Status == spacemeshv1.Layer_LAYER_STATUS_CONFIRMED {
			tctx.Log.Debugw("confirmed layer",
				"layer", layer.Layer.Number.Number,
				"hash", prettyHex(layer.Layer.Hash),
			)
			if layer.Layer.Number.Number == 20 {
				return false, nil
			}
		}
		return true, nil
	})
	require.NoError(t, eg.Wait())
}
