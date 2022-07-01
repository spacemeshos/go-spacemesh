package tests

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
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

func TestStepShortDisconnect(t *testing.T) {
	tctx := testcontext.New(t)
	cl, err := cluster.Reuse(tctx, cluster.WithKeys(10))
	require.NoError(t, err)

	var (
		enable = maxLayer(currentLayer(tctx, t, cl.Client(0))+2, 9)
		stop   = enable + 2
	)
	split := int(0.9 * float64(cl.Total()))

	eg, ctx := errgroup.WithContext(tctx)
	client := cl.Client(0)
	scheduleChaos(ctx, eg, client, enable, stop, func(ctx context.Context) (chaos.Teardown, error) {
		var left, right []string
		for i := 0; i < cl.Total(); i++ {
			if i < split {
				left = append(left, cl.Client(i).Name)
			} else {
				right = append(right, cl.Client(i).Name)
			}
		}
		tctx.Log.Debugw("short partition",
			"enable", enable,
			"stop", stop,
			"left", left,
			"right", right,
		)
		return chaos.Partition2(tctx, "split", left, right)
	})
	require.NoError(t, eg.Wait())
}

func TestStepTransactions(t *testing.T) {
	const (
		batch        = 100
		amount_limit = 100_000
	)
	rng := rand.New(rand.NewSource(time.Now().Unix()))

	tctx := testcontext.New(t)
	cl, err := cluster.Reuse(tctx, cluster.WithKeys(10))
	require.NoError(t, err)

	clients := make([]*txClient, cl.Accounts())
	synced := syncedNodes(tctx, cl)
	require.GreaterOrEqual(t, len(synced), tctx.ClusterSize/2)

	for i := range clients {
		clients[i] = &txClient{
			account: cl.Account(i),
			node:    synced[i%len(synced)],
		}
	}

	var eg errgroup.Group
	for _, client := range clients {
		client := client
		n := rng.Intn(batch) + batch
		eg.Go(func() error {
			nonce, err := client.nonce(tctx)
			require.NoError(t, err)
			if nonce == 0 {
				tctx.Log.Debugw("spawning wallet", "address", client.account)
				ctx, cancel := context.WithTimeout(tctx, 5*time.Minute)
				defer cancel()
				req, err := client.submit(ctx, wallet.SelfSpawn(client.account.PrivateKey))
				if err != nil {
					return err
				}
				if err := req.wait(ctx); err != nil {
					return err
				}
				nonce++

				rst, err := req.result(ctx)
				if err != nil {
					return err
				}

				tctx.Log.Debugw("spawned wallet", "address", client.account, "layer", rst.Layer)
			}
			tctx.Log.Debugw("submitting transactions",
				"address", client.account,
				"nonce", nonce,
				"count", n,
			)
			for i := 0; i < n; i++ {
				receiver := types.Address{}
				rng.Read(receiver[:])
				_, err := client.submit(tctx, wallet.Spend(
					client.account.PrivateKey,
					types.Address(receiver),
					rng.Uint64()%amount_limit,
					types.Nonce{Counter: nonce},
				))
				if err != nil {
					return err
				}
				nonce++
			}
			tctx.Log.Debugw("submitted transactions",
				"address", client.account,
				"nonce", nonce,
			)
			return nil
		})
	}
	require.NoError(t, eg.Wait())
}

func TestStepAddNodes(t *testing.T) {
	cctx := testcontext.New(t)
	cl, err := cluster.Reuse(cctx, cluster.WithKeys(10))
	require.NoError(t, err)

	delta := rand.Intn((cctx.ClusterSize*2/10)+1) + 1 // [1, 20%]
	delta = min(delta, cctx.ClusterSize-cl.Total())
	log := cctx.Log.With(
		"current", cl.Total(),
		"max", cctx.ClusterSize,
		"delta", delta,
	)
	if cl.Total()+delta >= cctx.ClusterSize {
		log.Warn("reached cluster limit. delete nodes before adding them")
		return
	}

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

	log.Info("deleting smeshers")
	require.NoError(t, cl.DeleteSmeshers(cctx, delta))
}

func TestStepVerify(t *testing.T) {
	cctx := testcontext.New(t)
	cl, err := cluster.Reuse(cctx, cluster.WithKeys(10))
	require.NoError(t, err)

	synced := syncedNodes(cctx, cl)
	require.GreaterOrEqual(t, len(synced), cctx.ClusterSize/2)

	require.Eventually(t, func() bool {
		var eg errgroup.Group
		for _, node := range synced {
			node := node
			eg.Go(func() error {
				return nil
			})
		}
		return eg.Wait() == nil
	}, 30*time.Minute, time.Minute)
}
