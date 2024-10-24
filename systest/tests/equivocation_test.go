package tests

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

// TestEquivocation runs a network where ~40% of nodes are expected to be equivocated due to shared keys.
func TestEquivocation(t *testing.T) {
	t.Parallel()
	cctx := testcontext.New(t)
	if cctx.PoetSize < 2 {
		t.Skip("Skipping test for using different poets - test configured with less than 2 poets")
	}

	const bootnodes = 2
	cl := cluster.New(cctx, cluster.WithKeys(cctx.ClusterSize))
	require.NoError(t, cl.AddBootnodes(cctx, bootnodes))
	require.NoError(t, cl.AddBootstrappers(cctx))
	require.NoError(t, cl.AddPoets(cctx))

	smeshers := cctx.ClusterSize - cl.Total()
	honest := int(float64(smeshers) * 0.6)
	if (smeshers-honest)%2 != 0 {
		honest++
	}
	malicious := smeshers - honest

	keys := make([]ed25519.PrivateKey, honest)
	for i := range honest {
		_, priv, err := ed25519.GenerateKey(nil)
		require.NoError(t, err)
		keys[i] = priv
	}
	malfeasants := make([]ed25519.PrivateKey, malicious/2)
	for i := range malicious / 2 {
		_, priv, err := ed25519.GenerateKey(nil)
		require.NoError(t, err)
		malfeasants[i] = priv
	}
	cctx.Log.Infow("fraction of nodes will have keys set up for equivocations",
		zap.Int("honest", honest),
		zap.Int("equivocators", malicious),
	)
	require.NoError(t, cl.AddSmeshers(cctx, honest, cluster.WithSmeshers(keys)))
	for _, key := range malfeasants {
		// ensure that the two nodes sharing the same key are using different poet endpoints so they
		// generate different proofs (otherwise they will be perfectly synchronized and won't trigger an equivocation)
		err := cl.AddSmeshers(cctx,
			1,
			cluster.WithSmeshers([]ed25519.PrivateKey{key}),
			cluster.NoDefaultPoets(),
			cluster.WithFlags(cluster.PoetEndpoints(1)),
		)
		require.NoError(t, err)
		err = cl.AddSmeshers(cctx,
			1,
			cluster.WithSmeshers([]ed25519.PrivateKey{key}),
			cluster.NoDefaultPoets(),
			cluster.WithFlags(cluster.PoetEndpoints(2)),
		)
		require.NoError(t, err)
	}

	var (
		layers    = uint32(testcontext.LayersPerEpoch.Get(cctx.Parameters))
		startTest = 2 * layers
		stopTest  = startTest + 4*layers

		eg      errgroup.Group
		mu      sync.Mutex
		results = make(map[string]map[int]string)
	)
	for i := 0; i < cl.Total(); i++ {
		client := cl.Client(i)
		results[client.Name] = make(map[int]string)
		watchLayers(cctx, &eg, client, cctx.Log.Desugar(), func(resp *pb.LayerStreamResponse) (bool, error) {
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
	require.NoError(t, eg.Wait())
	reference := results[cl.Client(0).Name]
	for i := 1; i < cl.Total(); i++ {
		assert.Equal(t, reference, results[cl.Client(i).Name],
			"reference: %v, client: %v", cl.Client(0).Name, cl.Client(i).Name,
		)
	}

	expected := make([]types.NodeID, 0, len(malfeasants))
	for _, key := range malfeasants {
		expected = append(expected, types.NodeID(key.Public().(ed25519.PublicKey)))
	}
	for i := 0; i < honest; i++ {
		client := cl.Client(i)
		proofs := make([]types.NodeID, 0, len(malfeasants))
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		malfeasanceStream(ctx, client, cctx.Log.Desugar(), func(malf *pb.MalfeasanceStreamResponse) (bool, error) {
			malfeasant := malf.GetProof().GetSmesherId().Id
			proofs = append(proofs, types.NodeID(malfeasant))
			return len(proofs) < len(malfeasants), nil
		})
		assert.ElementsMatchf(t, expected, proofs, "client: %s", cl.Client(i).Name)
	}
}
