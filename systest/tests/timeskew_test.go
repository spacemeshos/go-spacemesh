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
	const (
		enableSkew = 10
		stopSkew   = enableSkew + 3
		stopTest   = stopSkew + 2
		skewOffset = "-3s" // hare round is 2s
	)
	tctx := testcontext.New(t, testcontext.Labels("sanity"))
	cl, err := cluster.Default(tctx, cluster.WithKeys(10))
	require.NoError(t, err)

	failed := int(0.2 * float64(tctx.ClusterSize))
	eg, ctx := errgroup.WithContext(tctx)
	client := cl.Client(0)
	scheduleChaos(ctx, eg, client, enableSkew, stopSkew, func(ctx context.Context) (chaos.Teardown, error) {
		names := []string{}
		for i := 1; i <= failed; i++ {
			names = append(names, cl.Client(cl.Total()-i).Name)
		}
		return chaos.Timeskew(tctx, "skew", skewOffset, names...)
	})
	watchLayers(ctx, eg, client, func(layer *spacemeshv1.LayerStreamResponse) (bool, error) {
		if layer.Layer.Status == spacemeshv1.Layer_LAYER_STATUS_CONFIRMED {
			tctx.Log.Debugw("confirmed layer",
				"layer", layer.Layer.Number.Number,
				"hash", prettyHex(layer.Layer.Hash),
			)
			if layer.Layer.Number.Number == stopTest {
				return false, nil
			}
		}
		return true, nil
	})
	require.NoError(t, eg.Wait())
}
