package tests

import (
	"context"
	"strings"
	"testing"

	spacemeshv1 "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/systest/chaos"
	"github.com/spacemeshos/go-spacemesh/systest/cluster"
	"github.com/spacemeshos/go-spacemesh/systest/testcontext"
)

// nondoc change

func init() {
	// systest runs with `fastnet` preset. this init need to generate addresses with same hrp network prefix as fastnet.
	types.DefaultTestAddressConfig()
}

func TestAddNodes(t *testing.T) {
	t.Parallel()

	tctx := testcontext.New(t, testcontext.Labels("sanity"))

	const (
		epochBeforeJoin = 5
		lastEpoch       = 9

		beforeAdding = 12

		addedLater = 2
	)

	cl := cluster.New(tctx)
	total := min(tctx.ClusterSize, 30)

	require.NoError(t, cl.AddBootnodes(tctx, 2))
	require.NoError(t, cl.AddPoets(tctx))
	require.NoError(t, cl.AddSmeshers(tctx, total-2-addedLater))

	var eg errgroup.Group
	{
		watchLayers(tctx, &eg, cl.Client(0), func(layer *spacemeshv1.LayerStreamResponse) (bool, error) {
			if layer.Layer.Number.Number >= beforeAdding {
				tctx.Log.Debugw("adding new smeshers",
					"n", addedLater,
					"layer", layer.Layer.Number,
				)
				return false, cl.AddSmeshers(tctx, addedLater)
			}
			return true, nil
		})
	}
	require.NoError(t, eg.Wait())

	created := make([][]*spacemeshv1.Proposal, cl.Total())
	for i := 0; i < cl.Total(); i++ {
		i := i
		client := cl.Client(i)
		watchProposals(tctx, &eg, cl.Client(i), func(proposal *spacemeshv1.Proposal) (bool, error) {
			if proposal.Epoch.Value > lastEpoch {
				return false, nil
			}
			if proposal.Status == spacemeshv1.Proposal_Created {
				tctx.Log.Debugw("received proposal event",
					"client", client.Name,
					"layer", proposal.Layer.Number,
					"epoch", proposal.Epoch.Value,
					"smesher", prettyHex(proposal.Smesher.Id),
					"eligibilities", len(proposal.Eligibilities),
					"status", spacemeshv1.Proposal_Status_name[int32(proposal.Status)],
				)
				created[i] = append(created[i], proposal)
			}
			return true, nil
		})
	}
	require.NoError(t, eg.Wait())
	unique := map[uint64]map[string]struct{}{}
	for _, proposals := range created {
		for _, proposal := range proposals {
			if _, exist := unique[proposal.Epoch.Value]; !exist {
				unique[proposal.Epoch.Value] = map[string]struct{}{}
			}
			unique[proposal.Epoch.Value][prettyHex(proposal.Smesher.Id)] = struct{}{}
		}
	}
	for epoch := uint64(4); epoch <= epochBeforeJoin; epoch++ {
		require.GreaterOrEqual(t, len(unique[epoch]), cl.Total()-addedLater, "epoch=%d", epoch)
	}
	// condition is so that test waits until the first epoch where all smeshers participated.
	// and if it finds such epoch, starting from that epoch all smeshers should consistently
	// participate.
	// test should fail if such epoch wasn't found.
	var joined uint64
	for epoch := uint64(epochBeforeJoin) + 1; epoch <= lastEpoch; epoch++ {
		if len(unique[epoch]) == cl.Total() {
			joined = epoch
		}
		if joined != 0 && epoch >= joined {
			require.Len(t, unique[epoch], cl.Total(), "epoch=%d", epoch)
		}
	}
	require.NotEmpty(t, joined, "nodes weren't able to join the cluster")
}

func TestFailedNodes(t *testing.T) {
	t.Parallel()

	tctx := testcontext.New(t, testcontext.Labels("sanity"))

	const (
		failAt    = 15
		lastLayer = failAt + 8
	)

	cl, err := cluster.Default(tctx)
	require.NoError(t, err)

	failed := int(0.6 * float64(tctx.ClusterSize))

	eg, ctx := errgroup.WithContext(tctx)
	scheduleChaos(ctx, eg, cl.Client(0), failAt, lastLayer, func(ctx context.Context) (chaos.Teardown, error) {
		names := []string{}
		for i := 1; i <= failed; i++ {
			names = append(names, cl.Client(cl.Total()-i).Name)
		}
		tctx.Log.Debugw("failing nodes", "names", strings.Join(names, ","))
		return chaos.Fail(tctx, "fail60percent", names...)
	})

	hashes := make([]map[uint32]string, cl.Total())
	for i := 0; i < cl.Total(); i++ {
		hashes[i] = map[uint32]string{}
	}
	for i := 0; i < cl.Total()-failed; i++ {
		i := i
		client := cl.Client(i)
		watchLayers(ctx, eg, client, func(layer *spacemeshv1.LayerStreamResponse) (bool, error) {
			if layer.Layer.Status == spacemeshv1.Layer_LAYER_STATUS_APPLIED {
				tctx.Log.Debugw("layer applied",
					"client", client.Name,
					"layer", layer.Layer.Number.Number,
					"hash", prettyHex(layer.Layer.Hash),
				)
				if layer.Layer.Number.Number == lastLayer {
					return false, nil
				}
				hashes[i][layer.Layer.Number.Number] = prettyHex(layer.Layer.Hash)
			}
			return true, nil
		})
	}
	require.NoError(t, eg.Wait())
	reference := hashes[0]
	for i, tested := range hashes[1 : cl.Total()-failed] {
		assert.Equal(t, reference, tested, "client=%d", i)
	}
	require.NoError(t, waitAll(tctx, cl))
}
