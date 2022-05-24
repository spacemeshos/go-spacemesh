package tests

import (
	"context"
	"testing"

	"github.com/spacemeshos/go-spacemesh/systest/chaos"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

var (
	longevity = testcontext.Labels("longevity")
)

func TestStepCreate(t *testing.T) {
	ctx := testcontext.New(t, longevity)
	_, err := cluster.Reuse(ctx, cluster.WithKeys(10))
	require.NoError(t, err)
}

func TestStepDummy(t *testing.T) {
	tctx := testcontext.New(t, longevity)
	_, err := cluster.Reuse(tctx, cluster.WithKeys(10))
	require.NoError(t, err)
	tctx.Log.Infow("recovered cluster")
}

func TestStepChaos(t *testing.T) {
	tctx := testcontext.New(t, longevity)
	cl, err := cluster.Reuse(tctx, cluster.WithKeys(10))
	require.NoError(t, err)

	var (
		enableSkew = maxLayer(currentLayer(tctx, t, cl.Client(0))+2, 9)
		stopSkew   = enableSkew + 2
		skewOffset = "-3s" // hare round is 2s
	)
	tctx.Log.Debugw("enabled timeskew chaos",
		"enable", enableSkew,
		"stop", stopSkew,
	)

	failed := int(0.1 * float64(tctx.ClusterSize))
	eg, ctx := errgroup.WithContext(tctx)
	client := cl.Client(0)
	scheduleChaos(ctx, eg, client, enableSkew, stopSkew, func(ctx context.Context) (chaos.Teardown, error) {
		names := []string{}
		for i := 1; i <= failed; i++ {
			names = append(names, cl.Client(cl.Total()-i).Name)
		}
		return chaos.Timeskew(tctx, "skew", skewOffset, names...)
	})
}

// func TestStepTransactions(t *testing.T) {
// 	tctx := testcontext.New(t, longevity)
// 	cl, err := cluster.Reuse(tctx, cluster.WithKeys(10))
// 	require.NoError(t, err)

// 	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
// 	n := rng.Intn(10)

// }
