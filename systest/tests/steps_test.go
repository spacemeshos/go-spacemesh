package tests

import (
	"context"
	"encoding/hex"
	"math/rand"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/systest/chaos"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func TestStepCreate(t *testing.T) {
	ctx := testcontext.New(t)
	_, err := cluster.Reuse(ctx, cluster.WithKeys(10))
	require.NoError(t, err)
}

func TestStepChaos(t *testing.T) {
	tctx := testcontext.New(t)
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

func TestStepSubmitTransactions(t *testing.T) {
	tctx := testcontext.New(t)
	cl, err := cluster.Reuse(tctx, cluster.WithKeys(10))
	require.NoError(t, err)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	n := rng.Intn(cl.Accounts()-4) + 2 // send transaction from [2, 5] accounts

	tctx.Log.Debugw("submitting transactions", "accounts", n)

	// this test expected to tolerate clients failures.
	for i := 0; i < n; i++ {

	}
}

func TestStepAddNodes(t *testing.T) {
	cctx := testcontext.New(t)
	cl, err := cluster.Reuse(cctx, cluster.WithKeys(10))
	require.NoError(t, err)

	delta := rand.Intn((cctx.ClusterSize*2/10)+1) + 1 // [1, 20%]

	log := cctx.Log.With(
		"current", cl.Total(),
		"max", cctx.ClusterSize,
		"delta", delta,
	)

	log.Info("increasing cluster size")
	require.NoError(t, cl.AddSmeshers(cctx, delta))
}

func TestStepDeleteNodes(t *testing.T) {
	cctx := testcontext.New(t)
	cl, err := cluster.Reuse(cctx, cluster.WithKeys(10))
	require.NoError(t, err)

	delta := rand.Intn((cctx.ClusterSize*2/10)+1) + 1 // [1, 20%]

	log := cctx.Log.With(
		"current", cl.Total(),
		"max", cctx.ClusterSize,
		"delta", delta,
	)

	log.Info("increasing cluster size")
	require.NoError(t, cl.DeleteSmeshers(cctx, delta))
}

func TestStepVerifyBalances(t *testing.T) {
	cctx := testcontext.New(t)
	cl, err := cluster.Reuse(cctx, cluster.WithKeys(10))
	require.NoError(t, err)

	var (
		eg       errgroup.Group
		balances = make([]map[string]uint64, cl.Total())
	)
	for i := 0; i < cl.Total(); i++ {
		i := i
		client := cl.Client(i)
		eg.Go(func() error {
			state := map[string]uint64{}
			for j := 0; j < cl.Accounts(); j++ {
				addrbuf := cl.Address(j)
				ctx, cancel := context.WithTimeout(cctx, 2*time.Second)
				defer cancel()
				// TODO extend API to accept a layer, otherwise there concurrency problems
				balance, err := getAppliedBalance(ctx, client, addrbuf)
				if err != nil {
					cctx.Log.Errorw("requests to client are failing",
						"client", client.Name,
						"error", err,
					)
					return nil
				}
				address := hex.EncodeToString(addrbuf)

				state[address] = balance
			}
			balances[i] = state
			return nil
		})
	}
	require.NoError(t, eg.Wait())

	reference := balances[0]
	for addr, balance := range reference {
		cctx.Log.Infow("reference state",
			"address", addr,
			"balance", balance,
			"client", cl.Client(0).Name,
		)
	}
	for i := 1; i < cl.Total(); i++ {
		if balances[i] != nil {
			require.Equal(t, reference, balances[i], "client %d - %s", i, cl.Client(i).Name)
		}
	}

}
