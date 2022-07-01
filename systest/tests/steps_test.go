package tests

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	spacemeshv1 "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/systest/chaos"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"

	"github.com/stretchr/testify/assert"
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
	require.NoError(t, waitGenesis(tctx, cl.Client(0)))

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

	reference, err := getVerifiedLayer(cctx, synced[0])
	require.NoError(t, err)
	cctx.Log.Debugw("using verified layer as a reference",
		"node", synced[0].Name,
		"layer", reference.Number.Number,
		"hash", prettyHex(reference.Hash),
		"state hash", prettyHex(reference.RootStateHash),
	)
	layers := make([]*spacemeshv1.Layer, len(synced))

	// eventually because we don't want to fail whole test
	// if one of the nodes are slightly behind
	assert.Eventually(t, func() bool {
		var eg errgroup.Group
		for i, node := range synced {
			if i == 0 {
				continue
			}
			i := i
			node := node
			eg.Go(func() error {
				layer, err := getLayer(cctx, node, reference.Number.Number)
				if err != nil {
					return err
				}
				layers[i] = layer
				if bytes.Compare(layer.Hash, reference.Hash) != 0 {
					return fmt.Errorf("hash doesn't match reference")
				}
				if bytes.Compare(layer.RootStateHash, reference.RootStateHash) != 0 {
					return fmt.Errorf("state hash doesn't match reference")
				}
				return nil
			})
		}
		if err := eg.Wait(); err != nil {
			cctx.Log.Warnw("inconsistent cluster state", "error", err)
			return false
		}
		return true
	}, 30*time.Minute, time.Second)
	for i, layer := range layers {
		if i == 0 {
			continue
		}
		require.NotNil(t, layer, "client %s doesn't have layer %d",
			synced[i].Name, reference.Number)
		require.Equal(t, reference.Hash, layer.Hash, "consensus hash on client %s",
			synced[i].Name)
		require.Equal(t, reference.RootStateHash, layer.RootStateHash, "state hash on client %s",
			synced[i].Name)
	}
}

func TestScheduleBasicSteps(t *testing.T) {
	var (
		eg  errgroup.Group
		mu  sync.RWMutex
		rng = rand.New(rand.NewSource(time.Now().Unix()))
	)
	t.Run("create", TestStepCreate)
	eg.Go(func() error {
		for {
			time.Sleep(30 * time.Second)
			mu.RLock()
			t.Run("txs", TestStepTransactions)
			mu.RUnlock()
		}
	})
	eg.Go(func() error {
		for {
			time.Sleep(10 * time.Minute)
			mu.RLock()
			t.Run("verify", TestStepVerify)
			mu.RUnlock()
		}
	})
	eg.Go(func() error {
		for {
			time.Sleep(60 * time.Hour)
			mu.Lock()
			if rng.Int()%2 == 0 {
				t.Run("add", TestStepAddNodes)
			} else {
				t.Run("delete", TestStepDeleteNodes)
			}
			mu.Unlock()
		}
	})
	require.NoError(t, eg.Wait())
}
