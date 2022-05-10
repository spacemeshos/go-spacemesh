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
		enableSkew = 9
		stopSkew   = enableSkew + 2
		stopTest   = stopSkew + 5
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

	// hare round is 2s, including time when nodes wait for proposals
	// those nodes where clock is adjusted won't be able to reach consensus,
	// because they rounds time will not intersect with the rest of the cluster
	//
	// there are two possible failure scenarios:
	// in 1st 20% of the skewed nodes are not leaders, and they will simply cast abstain
	// votes for layer 11 and 12. it may delay layer confirmation, by no more than one layer
	// in 2nd - some nodes might be leaders, and in such case whole cluster will have to
	// abstain on one or two layers. in such case longer delay might be necessary to confirm that layer

	var confirmed uint32
	watchLayers(ctx, eg, client, func(layer *spacemeshv1.LayerStreamResponse) (bool, error) {
		if layer.Layer.Number.Number == stopTest {
			return false, nil
		}
		if layer.Layer.Status == spacemeshv1.Layer_LAYER_STATUS_CONFIRMED {
			tctx.Log.Debugw("confirmed layer",
				"layer", layer.Layer.Number.Number,
				"hash", prettyHex(layer.Layer.Hash),
			)
			confirmed = layer.Layer.Number.Number
		}
		return true, nil
	})
	require.NoError(t, eg.Wait())
	require.LessOrEqual(t, stopSkew, int(confirmed))
}
