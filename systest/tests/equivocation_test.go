package tests

import (
	"crypto/ed25519"
	"math/rand"
	"testing"

	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestEquivocation(t *testing.T) {
	t.Parallel()
	const bootnodes = 2
	cctx := testcontext.New(t, testcontext.Labels("sanity"))

	rng := rand.New(rand.NewSource(1111))
	keys := make([]ed25519.PrivateKey, cctx.ClusterSize-bootnodes)
	honest := len(keys) * 2 / 3
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
	cctx.Log.Infow("fraction of nodes will have keys set for equivocation",
		zap.Int("honest", honest),
		zap.Int("equivocators", len(keys)-honest),
	)
	cl := cluster.New(cctx, cluster.WithKeys(10))
	require.NoError(t, cl.AddBootnodes(cctx, bootnodes))
	require.NoError(t, cl.AddBootstrappers(cctx))
	require.NoError(t, cl.AddPoets(cctx))
	require.NoError(t, cl.AddSmeshers(cctx, cctx.ClusterSize-bootnodes, cluster.WithSmeshers(keys)))
}
