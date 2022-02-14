package tests

import (
	"context"
	"testing"
	"time"

	spacemeshv1 "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/systest/chaos"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

func TestPartition(t *testing.T) {
	tctx := testcontext.New(t, testcontext.Labels("sanity"))

	const (
		smeshers  = 7
		partition = 13
		restore   = 20
		wait      = 50
	)

	cl, err := cluster.Default(tctx,
		cluster.WithSmesherFlag(cluster.RerunInterval(2*time.Minute)),
	)
	require.NoError(t, err)

	hashes := make([]map[uint32]string, cl.Total())
	for i := 0; i < cl.Total(); i++ {
		hashes[i] = map[uint32]string{}
	}
	eg, ctx := errgroup.WithContext(tctx)

	scheduleChaos(ctx, eg, cl.Client(0), partition, restore, func(ctx context.Context) (chaos.Teardown, error) {
		return chaos.Partition2(tctx, "partition5from2",
			extractNames(cl.Client(0), cl.Client(2), cl.Client(3), cl.Client(4), cl.Client(5)),
			extractNames(cl.Client(1), cl.Client(6)),
		)
	})
	for i := 0; i < cl.Total(); i++ {
		i := i
		client := cl.Client(i)
		collectLayers(ctx, eg, client, func(layer *spacemeshv1.LayerStreamResponse) (bool, error) {
			if layer.Layer.Status == spacemeshv1.Layer_LAYER_STATUS_CONFIRMED {
				tctx.Log.Debugw("confirmed layer",
					"client", client.Name,
					"layer", layer.Layer.Number.Number,
					"hash", prettyHex(layer.Layer.Hash),
				)
				if layer.Layer.Number.Number == wait {
					return false, nil
				}
				hashes[i][layer.Layer.Number.Number] = prettyHex(layer.Layer.Hash)
			}
			return true, nil
		})
	}
	require.NoError(t, eg.Wait())
	reference := hashes[0]
	for i, tested := range hashes[1:] {
		assert.Equal(t, reference, tested, "client=%s", cl.Client(i+1).Name)
	}
}
