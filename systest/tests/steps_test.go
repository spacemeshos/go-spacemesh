package tests

import (
	"context"
	"encoding/hex"
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

func TestStepPartition(t *testing.T) {
	tctx := testcontext.New(t)
	cl, err := cluster.Reuse(tctx, cluster.WithKeys(10))
	require.NoError(t, err)

	var (
		enable = maxLayer(currentLayer(tctx, t, cl.Client(0))+2, 9)
		stop   = enable + 1
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
	// TODO filter out node clients that are are not yet synced
	for i := range clients {
		clients[i] = &txClient{
			account: cl.Account(i),
			node:    cl.Client(i % cl.Total()),
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
		log.Error("reached cluster limit. delete nodes before adding them")
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

func TestStepVerifyResults(t *testing.T) {
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
