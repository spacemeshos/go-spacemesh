package tests

import (
	"context"
	"crypto/ed25519"
	"math/rand"
	"sync"
	"testing"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/systest/chaos"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

func TestEquivocation(t *testing.T) {
	t.Parallel()
	const bootnodes = 2
	cctx := testcontext.New(t, testcontext.Labels("sanity"))

	rng := rand.New(rand.NewSource(1111))
	keys := make([]ed25519.PrivateKey, cctx.ClusterSize-bootnodes)
	honest := len(keys) / 2
	if (len(keys)-honest)%2 != 0 {
		honest--
	}
	for i := 0; i < honest; i++ {
		_, priv, err := ed25519.GenerateKey(rng)
		require.NoError(t, err)
		keys[i] = priv
	}
	for i := honest; i < len(keys); i += 2 {
		_, priv, err := ed25519.GenerateKey(rng)
		require.NoError(t, err)
		keys[i] = priv
		keys[i+1] = priv
	}
	cctx.Log.Infow("fraction of nodes will have keys set up for equivocations",
		zap.Int("honest", honest),
		zap.Int("equivocators", len(keys)-honest),
	)
	cl := cluster.New(cctx, cluster.WithKeys(10))
	require.NoError(t, cl.AddBootnodes(cctx, bootnodes))
	require.NoError(t, cl.AddBootstrappers(cctx))
	require.NoError(t, cl.AddPoets(cctx))
	require.NoError(t, cl.AddSmeshers(cctx, cctx.ClusterSize-bootnodes, cluster.WithSmeshers(keys)))

	var (
		layers         = uint32(testcontext.LayersPerEpoch.Get(cctx.Parameters))
		startPartition = 2 * layers
		stopPartition  = startPartition + layers
		stopTest       = stopPartition + 4*layers

		eg          errgroup.Group
		left, right []string
	)
	for i := 0; i < cl.Total(); i += 2 {
		left = append(left, cl.Client(i).Name)
		if i+1 != cl.Total() {
			right = append(right, cl.Client(i+1).Name)
		}
	}
	scheduleChaos(cctx, &eg, cl.Client(0), startPartition, stopPartition, func(ctx context.Context) (chaos.Teardown, error) {
		cctx.Log.Infow("partition honest with equivocators",
			"start", startPartition,
			"stop", stopPartition,
			"left", left,
			"right", right,
		)
		return chaos.Partition2(cctx, "split-with-equivocators", left, right)
	})

	var (
		mu      sync.Mutex
		results = map[string]map[int]string{}
	)
	for i := 0; i < cl.Total(); i++ {
		client := cl.Client(i)
		results[client.Name] = map[int]string{}
		watchLayers(cctx, &eg, client, func(resp *pb.LayerStreamResponse) (bool, error) {
			if resp.Layer.Status != pb.Layer_LAYER_STATUS_APPLIED {
				return true, nil
			}
			if resp.Layer.Number.Number > stopTest {
				return false, nil
			}
			num := int(resp.Layer.Number.Number)
			consensus := types.BytesToHash(resp.Layer.Hash).ShortString()
			cctx.Log.Debugw("consensus hash collected",
				"client", client.Name,
				"layer", num,
				"consensus", consensus,
			)
			mu.Lock()
			results[client.Name][num] = consensus
			mu.Unlock()
			return true, nil
		})
	}
	eg.Wait()
	reference := results[cl.Client(0).Name]
	for i := 1; i < cl.Total(); i++ {
		require.Equal(t, reference, results[cl.Client(i).Name],
			"reference: %v, client: %v", cl.Client(0).Name, cl.Client(i).Name)
	}
}
