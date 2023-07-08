package tests

import (
	"crypto/ed25519"
	"sync"
	"testing"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

func TestEquivocation(t *testing.T) {
	t.Parallel()
	const bootnodes = 2
	cctx := testcontext.New(t, testcontext.Labels("sanity"))

	keys := make([]ed25519.PrivateKey, cctx.ClusterSize-bootnodes)
	honest := int(float64(len(keys)) * 0.6)
	if (len(keys)-honest)%2 != 0 {
		honest++
	}
	for i := 0; i < honest; i++ {
		_, priv, err := ed25519.GenerateKey(nil)
		require.NoError(t, err)
		keys[i] = priv
	}
	for i := honest; i < len(keys); i += 2 {
		_, priv, err := ed25519.GenerateKey(nil)
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
		layers    = uint32(testcontext.LayersPerEpoch.Get(cctx.Parameters))
		startTest = 2 * layers
		stopTest  = startTest + 4*layers

		eg      errgroup.Group
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
		assert.Equal(t, reference, results[cl.Client(i).Name],
			"reference: %v, client: %v", cl.Client(0).Name, cl.Client(i).Name)
	}
}
