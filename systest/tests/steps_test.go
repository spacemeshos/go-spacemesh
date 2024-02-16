package tests

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/systest/chaos"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
	"github.com/spacemeshos/go-spacemesh/systest/validation"
)

func TestStepCreate(t *testing.T) {
	ctx := testcontext.New(t, testcontext.SkipClusterLimits())
	_, err := cluster.Reuse(ctx, cluster.WithKeys(ctx.ClusterSize))
	require.NoError(t, err)
}

func TestStepShortDisconnect(t *testing.T) {
	tctx := testcontext.New(t, testcontext.SkipClusterLimits())
	cl, err := cluster.Reuse(tctx, cluster.WithKeys(tctx.ClusterSize))
	require.NoError(t, err)
	require.Greater(t, cl.Bootnodes(), 1)

	var (
		enable = max(currentLayer(tctx, t, cl.Client(0))+2, 9)
		stop   = enable + 2
	)
	// make sure the first boot node is in the 2nd partition so the poet proof can be broadcast to both splits
	split := int(0.9*float64(cl.Total())) + 1

	eg, ctx := errgroup.WithContext(tctx)
	client := cl.Client(0)
	scheduleChaos(
		ctx,
		eg,
		client,
		tctx.Log.Desugar(),
		enable,
		stop,
		func(ctx context.Context) (chaos.Teardown, error) {
			var (
				left  []string
				right = []string{cl.Client(0).Name}
			)
			for i := 1; i < cl.Total(); i++ {
				if i < split {
					left = append(left, cl.Client(i).Name)
				} else {
					right = append(right, cl.Client(i).Name)
				}
			}
			tctx.Log.Debugw("short partition", "enable", enable, "stop", stop, "left", left, "right", right)
			return chaos.Partition2(tctx, "split", left, right)
		},
	)
	require.NoError(t, eg.Wait())
}

func TestStepTransactions(t *testing.T) {
	const (
		batch       = 10
		amountLimit = 100_000
	)

	tctx := testcontext.New(t, testcontext.SkipClusterLimits())
	cl, err := cluster.Reuse(tctx, cluster.WithKeys(tctx.ClusterSize))
	require.NoError(t, err)
	require.NoError(t, waitGenesis(tctx, cl.Client(0)))
	t.Cleanup(cl.CloseClients)

	clients := make([]*txClient, cl.Accounts())
	synced := syncedNodes(tctx, cl)

	for i := range clients {
		clients[i] = &txClient{
			account: cl.Account(i),
			node:    synced[i%len(synced)],
		}
	}

	var eg errgroup.Group
	for i, client := range clients {
		eg.Go(func() error {
			rng := rand.New(rand.NewSource(time.Now().Unix() + int64(i)))
			n := rng.Intn(batch) + batch
			nonce, err := client.nonce(tctx)
			if err != nil {
				return err
			}
			if nonce == 0 {
				tctx.Log.Debugw("spawning wallet", "address", client.account)
				ctx, cancel := context.WithTimeout(tctx, 5*time.Minute)
				defer cancel()
				req, err := client.submit(
					ctx,
					wallet.SelfSpawn(client.account.PrivateKey, 0, sdk.WithGenesisID(cl.GenesisID())),
				)
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
				randBytes := [types.AddressLength]byte{}
				rng.Read(randBytes[:])
				receiver := types.GenerateAddress(randBytes[:])
				rng.Read(receiver[:])
				raw := wallet.Spend(
					client.account.PrivateKey,
					receiver,
					rng.Uint64()%amountLimit,
					nonce,
					sdk.WithGenesisID(cl.GenesisID()),
				)
				_, err := client.submit(tctx, raw)
				if err != nil {
					return fmt.Errorf("failed to submit 0x%x from %s with nonce %d: %w",
						hash.Sum(raw), client.account, nonce, err,
					)
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
	if err := eg.Wait(); err != nil {
		tctx.Log.Errorw("failed to submit transactions", "error", err)
	}
}

func TestStepReplaceNodes(t *testing.T) {
	cctx := testcontext.New(t, testcontext.SkipClusterLimits())
	cl, err := cluster.Reuse(cctx, cluster.WithKeys(cctx.ClusterSize))
	require.NoError(t, err)

	var (
		max      = cctx.ClusterSize * 2 / 10
		delete   = rand.Intn(max) + 1
		deleting []*cluster.NodeClient
	)
	for i := cl.Bootnodes(); i < cl.Total() && len(deleting) < delete; i++ {
		node := cl.Client(i)
		// don't replace non-synced nodes
		if !isSynced(cctx, node) {
			continue
		}
		deleting = append(deleting, node)
	}
	for _, node := range deleting {
		cctx.Log.Debugw("deleting smesher", "name", node.Name)
		require.NoError(t, cl.DeleteSmesher(cctx, node))
	}
	if len(deleting) > 0 {
		require.NoError(t, cl.AddSmeshers(cctx, len(deleting)))
	}
}

func TestStepVerifyConsistency(t *testing.T) {
	cctx := testcontext.New(t, testcontext.SkipClusterLimits())
	cl, err := cluster.Reuse(cctx, cluster.WithKeys(cctx.ClusterSize))
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
	layers := make([]*pb.Layer, len(synced))

	// eventually because we don't want to fail whole test
	// if one of the nodes are slightly behind
	assert.Eventually(t, func() bool {
		var eg errgroup.Group
		for i, node := range synced {
			if i == 0 {
				continue
			}
			eg.Go(func() error {
				layer, err := getLayer(cctx, node, reference.Number.Number)
				if err != nil {
					return err
				}
				layers[i] = layer
				if !bytes.Equal(layer.Hash, reference.Hash) {
					return fmt.Errorf("hash doesn't match reference %s in layer %d: %x != %x",
						node.Name, reference.Number.Number, layer.Hash, reference.Hash)
				}
				if !bytes.Equal(layer.RootStateHash, reference.RootStateHash) {
					return fmt.Errorf(
						"state hash doesn't match reference %s in layer %d: %x != %x",
						node.Name,
						reference.Number.Number,
						layer.RootStateHash,
						reference.RootStateHash,
					)
				}
				return nil
			})
		}
		if err := eg.Wait(); err != nil {
			cctx.Log.Warnw("inconsistent cluster state", "error", err)
			return false
		}
		return true
	}, 30*time.Minute, time.Minute)
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

func newRunner() *runner {
	return &runner{
		failed: make(chan struct{}),
	}
}

type runner struct {
	eg     errgroup.Group
	mu     sync.RWMutex
	failed chan struct{}
}

func (r *runner) run(period time.Duration, fn func() bool) {
	r.eg.Go(func() error {
		ticker := time.NewTicker(period)
		defer ticker.Stop()
		for {
			select {
			case <-r.failed:
				return nil
			case <-ticker.C:
			}
			success := fn()
			if !success {
				select {
				case <-r.failed:
				default:
					close(r.failed)
				}
			}
		}
	})
}

func (r *runner) wait() {
	r.eg.Wait()
}

func (r *runner) one(period time.Duration, fn func() bool) {
	r.run(period, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return fn()
	})
}

func (r *runner) concurrent(period time.Duration, fn func() bool) {
	r.run(period, func() bool {
		r.mu.RLock()
		defer r.mu.RUnlock()
		return fn()
	})
}

func TestScheduleBasic(t *testing.T) {
	TestStepCreate(t)
	rn := newRunner()
	rn.concurrent(30*time.Second, func() bool {
		return t.Run("txs", TestStepTransactions)
	})
	rn.concurrent(5*time.Minute, func() bool {
		return t.Run("verify", TestStepVerifyConsistency)
	})
	rn.one(60*time.Minute, func() bool {
		return t.Run("replace nodes", TestStepReplaceNodes)
	})
	rn.wait()
}

func TestScheduleTransactions(t *testing.T) {
	TestStepCreate(t)
	rn := newRunner()
	rn.concurrent(10*time.Second, func() bool {
		return t.Run("txs", TestStepTransactions)
	})
	rn.wait()
}

func TestStepValidation(t *testing.T) {
	tctx := testcontext.New(t, testcontext.SkipClusterLimits())
	c, err := cluster.Reuse(tctx, cluster.WithKeys(tctx.ClusterSize))
	require.NoError(t, err)

	eg, ctx := errgroup.WithContext(tctx)
	eg.Go(func() error {
		// ensure that node will be back to sync in 2 hours
		err := validation.Periodic(ctx, 12*time.Minute, validation.Sync(c, 10))
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return nil
	})
	eg.Go(func() error {
		// ensure that within 25 minutes that current-4 layer will be consistent across the cluster
		err := validation.Periodic(ctx, 5*time.Minute, validation.Consensus(c, 5, 4))
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	})
	require.NoError(t, eg.Wait())
}
